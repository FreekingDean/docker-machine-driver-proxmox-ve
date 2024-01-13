package driver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/FreekingDean/proxmox-api-go/proxmox"
	"github.com/FreekingDean/proxmox-api-go/proxmox/cluster"
	"github.com/FreekingDean/proxmox-api-go/proxmox/nodes/qemu"
	ignition "github.com/coreos/ignition/v2/config/v3_4/types"
)

// PreCreateCheck is called to enforce pre-creation steps
func (d *Driver) PreCreateCheck() error {
	_, err := d.EnsureClient()
	if err != nil {
		return err
	}

	return nil
}

// Create creates a new VM with storage
func (d *Driver) Create() error {
	d.debug("Creating Client")
	c, err := d.EnsureClient()
	if err != nil {
		return err
	}

	d.debug("getting next vmid")
	cclient := cluster.New(c)
	id, err := cclient.Nextid(context.Background(), cluster.NextidRequest{})
	if err != nil {
		return err
	}
	tvalue := true

	keys, err := d.importSSHKeys()
	if err != nil {
		return err
	}

	d.debug("gen keys")
	key, err := d.generateKey()
	if err != nil {
		return err
	}
	keys = append(keys, ignition.SSHAuthorizedKey(key))
	systemd := `
[Unit]
Description=Layer qemu-guest-agent with rpm-ostree
Wants=network-online.target
After=network-online.target
Before=zincati.service
ConditionPathExists=!/var/lib/%N.stamp

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/bin/rpm-ostree install --apply-live --allow-inactive qemu-guest-agent
ExecStart=/bin/systemctl --now enable qemu-guest-agent
ExecStart=/bin/touch /var/lib/%N.stamp

[Install]
WantedBy=multi-user.target
`
	cfg := &ignition.Config{
		Systemd: ignition.Systemd{
			Units: []ignition.Unit{
				ignition.Unit{
					Name:     "rpm-ostree-install-qemu-guest-agent.service",
					Enabled:  &tvalue,
					Contents: &systemd,
				},
			},
		},
		Passwd: ignition.Passwd{
			Users: []ignition.PasswdUser{
				ignition.PasswdUser{
					Name:              d.SSHUser,
					Groups:            []ignition.Group{"wheel", "sudo"},
					SSHAuthorizedKeys: keys,
				},
			},
		},
		Ignition: ignition.Ignition{
			Version: "3.4.0",
		},
	}

	cfgStr, err := json.Marshal(cfg)
	if err != nil {
		return err
	}

	d.debugf("Next ID is '%d'", id)
	fwstr := fmt.Sprintf(
		"name=opt/com.coreos/config,string='%s'",
		strings.Replace(string(cfgStr), ",", ",,", -1),
	)
	d.VMID = id
	req := qemu.CreateRequest{
		Vmid:   d.VMID,
		Args:   proxmox.String(fmt.Sprintf("-fw_cfg %s", fwstr)),
		Name:   proxmox.String(d.GetMachineName()),
		Node:   d.Node,
		Memory: proxmox.Int(d.Memory),
		Cores:  proxmox.Int(d.CPUCores),
		Pool:   proxmox.String(d.Pool),
		Nets: &qemu.Nets{
			&qemu.Net{
				Model:  qemu.NetModel_VIRTIO,
				Bridge: proxmox.String(d.NetBridge),
				Tag:    proxmox.Int(d.NetVlanTag),
			},
		},
		Agent: &qemu.Agent{
			Enabled: *proxmox.PVEBool(true),
		},
		Serials: &qemu.Serials{proxmox.String("socket")},
	}

	if d.Scsi != "" {
		d.debug("Adding scsi0")
		scsi := &qemu.Scsi{
			File: d.Scsi,
		}
		if d.ScsiImport != "" {
			d.debug("adding import")
			scsi.ImportFrom = proxmox.String(d.ScsiImport)
		}
		req.Scsis = &qemu.Scsis{scsi}
	}
	q := qemu.New(c)
	taskID, err := q.Create(context.Background(), req)
	if err != nil {
		return err
	}
	dangling := true
	defer func() {
		if dangling {
			err = d.Remove()
			if err != nil {
				d.debugf("Error removing dangling resource: %w", err)
			}
		}
	}()
	err = d.waitForTaskToComplete(taskID, 2*time.Minute)
	if err != nil {
		return err
	}

	// resize disk
	if d.ScsiDiskSize != 0 {
		// allow machine to settle
		time.Sleep(10 * time.Second)
		err = d.retry(func() error {
			return q.ResizeVm(context.Background(), qemu.ResizeVmRequest{
				Disk: "scsi0",
				Node: d.Node,
				Vmid: d.VMID,
				Size: fmt.Sprintf("%dG", d.ScsiDiskSize),
			})
		}, 10*time.Second, 10)
		if err != nil {
			return err
		}
	}

	// start the VM
	err = d.Start()
	if err != nil {
		return err
	}

	// let VM start a settle a little
	d.debugf("waiting for VM to start, wait 10 seconds")
	time.Sleep(10 * time.Second)

	// wait for network to come up
	err = d.waitForNetwork()
	if err != nil {
		return err
	}
	dangling = false
	return nil
}

// Remove removes the VM
func (d *Driver) Remove() error {
	if d.VMID < 1 {
		return nil
	}
	// force shut down VM before invoking delete
	err := d.Kill()
	if err != nil {
		d.debug("error stopping vm " + err.Error())
	}

	c, err := d.EnsureClient()
	if err != nil {
		return err
	}
	q := qemu.New(c)

	taskID, err := q.Delete(context.Background(), qemu.DeleteRequest{
		Vmid:                     d.VMID,
		Node:                     d.Node,
		DestroyUnreferencedDisks: proxmox.PVEBool(true),
		Purge:                    proxmox.PVEBool(true),
	})
	if err != nil {
		return err
	}
	return d.waitForTaskToComplete(taskID, 10*time.Minute)
}

func (d *Driver) waitForNetwork() error {
	// attempt over 5 minutes
	// time for startup, qemu install, and network to come online
	for i := 0; i < 60; i++ {
		ip, err := d.getVMIp()
		if err != nil {
			return err
		}
		if ip != "" {
			d.IPAddress = ip
			return nil
		}
		d.debugf("waiting for VM network to start")
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("failed waiting for IP")
}
