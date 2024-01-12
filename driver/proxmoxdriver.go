package driver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/docker/machine/libmachine/drivers"
	"github.com/docker/machine/libmachine/mcnflag"
	"github.com/docker/machine/libmachine/state"
	"github.com/labstack/gommon/log"

	"github.com/FreekingDean/proxmox-api-go/proxmox"
	"github.com/FreekingDean/proxmox-api-go/proxmox/access"
	"github.com/FreekingDean/proxmox-api-go/proxmox/cluster"
	"github.com/FreekingDean/proxmox-api-go/proxmox/nodes/qemu"
	"github.com/FreekingDean/proxmox-api-go/proxmox/nodes/qemu/agent"
	"github.com/FreekingDean/proxmox-api-go/proxmox/nodes/qemu/status"
	"github.com/FreekingDean/proxmox-api-go/proxmox/nodes/tasks"
	ignition "github.com/coreos/ignition/v2/config/v3_4/types"
)

// Driver for Proxmox VE
type Driver struct {
	*drivers.BaseDriver
	client *proxmox.Client

	// Top-level strategy for proisioning a new node
	ProvisionStrategy string

	// Basic Authentication for Proxmox VE
	Host     string // Host to connect to
	Node     string // optional, node to create VM on, host used if omitted but must match internal node name
	User     string // username
	Password string // password
	Realm    string // realm, e.g. pam, pve, etc.
	Pool     string // pool

	// File to load as boot image RancherOS/Boot2Docker

	Scsi       string //Scsi0 data
	ScsiImport string //Scsi0 Import
	Ide        string //Ide0 data
	Memory     int    // memory in GB

	NetBridge  string // bridge applied to network interface
	NetVlanTag int    // vlan tag

	VMID          int    // VM ID only filled by create()
	GuestUsername string // user to log into the guest OS to copy the public key
	CPUCores      int    // The number of cores per socket.
	driverDebug   bool   // driver debugging
}

func (d *Driver) debugf(format string, v ...interface{}) {
	if d.driverDebug {
		log.Infof(format, v...)
	}
}

func (d *Driver) debug(v ...interface{}) {
	if d.driverDebug {
		log.Info(v...)
	}
}

func (d *Driver) EnsureClient() (*proxmox.Client, error) {
	if d.client != nil {
		return d.client, nil
	}
	d.debugf("Create called")

	d.debugf("Connecting to %s as %s@%s with password '%s'", d.Host, d.User, d.Realm, d.Password)
	client := proxmox.NewClient(d.Host)
	a := access.New(client)
	ticket, err := a.CreateTicket(context.Background(), access.CreateTicketRequest{
		Username: d.User,
		Password: d.Password,
		Realm:    &d.Realm,
	})
	if err != nil {
		d.debugf("error retreiving ticket %s", err.Error())
		return nil, err
	}
	client.SetCookie(*ticket.Ticket)
	client.SetCsrf(*ticket.Csrfpreventiontoken)
	d.client = client
	return client, nil
}

// GetCreateFlags returns the argument flags for the program
func (d *Driver) GetCreateFlags() []mcnflag.Flag {
	return []mcnflag.Flag{
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_PROXMOX_HOST",
			Name:   "proxmoxve-proxmox-host",
			Usage:  "Host to connect to",
			Value:  "192.168.1.253",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_PROXMOX_NODE",
			Name:   "proxmoxve-proxmox-node",
			Usage:  "Node to use (defaults to host)",
			Value:  "",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_PROXMOX_USER_NAME",
			Name:   "proxmoxve-proxmox-user-name",
			Usage:  "User to connect as",
			Value:  "root",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_PROXMOX_USER_PASSWORD",
			Name:   "proxmoxve-proxmox-user-password",
			Usage:  "Password to connect with",
			Value:  "",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_PROXMOX_REALM",
			Name:   "proxmoxve-proxmox-realm",
			Usage:  "Realm to connect to (default: pam)",
			Value:  "pam",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_PROXMOX_POOL",
			Name:   "proxmoxve-proxmox-pool",
			Usage:  "pool to attach to",
			Value:  "",
		},
		mcnflag.IntFlag{
			EnvVar: "PROXMOXVE_VM_MEMORY",
			Name:   "proxmoxve-vm-memory",
			Usage:  "memory in GB",
			Value:  8,
		},
		mcnflag.IntFlag{
			EnvVar: "PROXMOXVE_VM_CPU_CORES",
			Name:   "proxmoxve-vm-cpu-cores",
			Usage:  "number of cpu cores",
			Value:  1,
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_VM_SCSI",
			Name:   "proxmoxve-vm-scsi",
			Usage:  "proxmox scsi0 filename",
			Value:  "",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_VM_SCSI_IMPORT",
			Name:   "proxmoxve-vm-scsi-import",
			Usage:  "proxmox scsi0 import-from",
			Value:  "",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_VM_ide",
			Name:   "proxmoxve-vm-ide",
			Usage:  "proxmox ide filename",
			Value:  "",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_VM_NET_BRIDGE",
			Name:   "proxmoxve-vm-net-bridge",
			Usage:  "bridge to attach network to",
			Value:  "vmbr0", // leave the flag default value blank to support the clone default behavior if not explicity set of 'use what is most appropriate'
		},
		mcnflag.IntFlag{
			EnvVar: "PROXMOXVE_VM_NET_TAG",
			Name:   "proxmoxve-vm-net-tag",
			Usage:  "vlan tag",
			Value:  0,
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_SSH_USERNAME",
			Name:   "proxmoxve-ssh-username",
			Usage:  "Username to log in to the guest OS (default docker for rancheros)",
			Value:  "",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_SSH_PASSWORD",
			Name:   "proxmoxve-ssh-password",
			Usage:  "Password to log in to the guest OS (default tcuser for rancheros)",
			Value:  "",
		},
		mcnflag.IntFlag{
			EnvVar: "PROXMOXVE_SSH_PORT",
			Name:   "proxmoxve-ssh-port",
			Usage:  "SSH port in the guest to log in to (defaults to 22)",
			Value:  22,
		},
		mcnflag.BoolFlag{
			EnvVar: "PROXMOXVE_DEBUG_DRIVER",
			Name:   "proxmoxve-debug-driver",
			Usage:  "enables debugging in the driver",
		},
	}
}

// DriverName returns the name of the driver
func (d *Driver) DriverName() string {
	return "proxmoxve"
}

// SetConfigFromFlags configures all command line arguments
func (d *Driver) SetConfigFromFlags(flags drivers.DriverOptions) error {
	d.debug("SetConfigFromFlags called")

	// PROXMOX API Connection settings
	d.Host = flags.String("proxmoxve-proxmox-host")
	d.Node = flags.String("proxmoxve-proxmox-node")

	d.User = flags.String("proxmoxve-proxmox-user-name")
	d.Password = flags.String("proxmoxve-proxmox-user-password")
	d.Realm = flags.String("proxmoxve-proxmox-realm")
	d.Pool = flags.String("proxmoxve-proxmox-pool")

	// VM configuration
	d.Memory = flags.Int("proxmoxve-vm-memory")
	d.Memory *= 1024
	d.GuestUsername = "docker"
	d.Scsi = flags.String("proxmoxve-vm-scsi")
	d.ScsiImport = flags.String("proxmoxve-vm-scsi-import")
	d.Ide = flags.String("proxmoxve-vm-ide")
	d.CPUCores = flags.Int("proxmoxve-vm-cpu-cores")
	d.NetBridge = flags.String("proxmoxve-vm-net-bridge")
	d.NetVlanTag = flags.Int("proxmoxve-vm-net-tag")

	//Debug option
	d.driverDebug = flags.Bool("proxmoxve-debug-driver")

	return nil
}

// GetURL returns the URL for the target docker daemon
func (d *Driver) GetURL() (string, error) {
	ip, err := d.GetIP()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("tcp://%s:2376", ip), nil
}

// GetMachineName returns the machine name
func (d *Driver) GetMachineName() string {
	return d.MachineName
}

// GetNetBridge returns the bridge
func (d *Driver) GetNetBridge() string {
	return d.NetBridge
}

// GetNetVlanTag returns the vlan tag
func (d *Driver) GetNetVlanTag() int {
	return d.NetVlanTag
}

// GetSSHHostname returns the ssh host returned by the API
func (d *Driver) GetSSHHostname() (string, error) {
	return d.GetIP()
}

// GetSSHPort returns the ssh port, 22 if not specified
func (d *Driver) GetSSHPort() (int, error) {
	return 22, nil
}

// GetSSHUsername returns the ssh user name, root if not specified
func (d *Driver) GetSSHUsername() string {
	return d.GuestUsername
}

// GetState returns the state of the VM
func (d *Driver) GetState() (state.State, error) {
	if d.VMID < 1 {
		return state.Error, errors.New("invalid VMID")
	}

	c, err := d.EnsureClient()
	if err != nil {
		return state.Error, err
	}

	s := status.New(c)
	resp, err := s.VmStatusCurrent(context.Background(), status.VmStatusCurrentRequest{
		Node: d.Node,
		Vmid: d.VMID,
	})
	if resp.Status == status.Status_STOPPED {
		return state.Stopped, nil
	}
	if resp.Status == status.Status_RUNNING {
		return state.Running, nil
	}
	return state.Error, nil
}

// PreCreateCheck is called to enforce pre-creation steps
func (d *Driver) PreCreateCheck() error {
	_, err := d.EnsureClient()
	if err != nil {
		return err
	}

	if len(d.NetBridge) < 1 {
		d.NetBridge = "vmbr0"
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

	d.debug("gen keys")
	key, err := d.generateKey()
	if err != nil {
		return err
	}
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
ExecStart=/usr/bin/rpm-ostree install --apply-live --allow-inactive qemu-guest-agent && systemctl --now enable qemu-guest-agent
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
					Name: "docker",
					SSHAuthorizedKeys: []ignition.SSHAuthorizedKey{
						ignition.SSHAuthorizedKey(key),
					},
				},
			},
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
	if d.Ide != "" {
		req.Ides = &qemu.Ides{
			&qemu.Ide{
				File: d.Ide,
			},
		}
	}
	q := qemu.New(c)
	_, err = q.Create(context.Background(), req)
	if err != nil {
		return err
	}
	dangling := true
	defer func() {
		if dangling {
			d.Remove()
		}
	}()

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

func (d *Driver) checkIP() (string, error) {
	d.debugf("checking for IP address")
	c, err := d.EnsureClient()
	if err != nil {
		return "", err
	}
	a := agent.New(c)
	resp, err := a.Create(context.Background(), agent.CreateRequest{
		Command: "network-get-interfaces",
		Node:    d.Node,
		Vmid:    d.VMID,
	})

	data, ok := resp["data"].(map[string][]map[string]interface{})
	if !ok {
		return "", nil
	}
	for _, nic := range data["result"] {
		name, ok := nic["name"].(string)
		if !ok {
			return "", nil
		}
		if name != "lo" {
			ips, ok := nic["ip-addresses"].([]map[string]string)
			if !ok {
				return "", nil
			}
			for _, ip := range ips {
				if ip["ip-address-type"] == "ipv4" && ip["ip-address"] != "127.0.0.1" {
					return "", nil
				}
			}
		}
	}
	return "", nil
}

func (d *Driver) waitForNetwork() error {
	// attempt over 5 minutes
	// time for startup, qemu install, and network to come online
	for i := 0; i < 60; i++ {
		ip, err := d.checkIP()
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

// Start starts the VM
func (d *Driver) Start() error {
	if d.VMID < 1 {
		return errors.New("invalid VMID")
	}

	c, err := d.EnsureClient()
	if err != nil {
		return err
	}

	s := status.New(c)
	taskID, err := s.VmStart(context.Background(), status.VmStartRequest{
		Node: d.Node,
		Vmid: d.VMID,
	})
	if err != nil {
		return err
	}

	return d.waitForTaskToComplete(taskID, 2*time.Minute)
}

func (d *Driver) waitForTaskToComplete(taskId string, dur time.Duration) error {
	c, err := d.EnsureClient()
	if err != nil {
		return err
	}

	t := tasks.New(c)

	endTime := time.Now().Add(dur)
	for !time.Now().After(endTime) {
		resp, err := t.ReadTaskStatus(
			context.Background(),
			tasks.ReadTaskStatusRequest{
				Node: d.Node,
				Upid: taskId,
			},
		)
		if err != nil {
			return err
		}
		if resp.Status != "running" {
			if resp.Exitstatus != nil && *resp.Exitstatus != "OK" {
				return fmt.Errorf("task failed '%v'", resp.Exitstatus)
			}
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("timed out waiting for task")
}

// Stop stopps the VM
func (d *Driver) Stop() error {
	if d.VMID < 1 {
		return errors.New("invalid VMID")
	}

	c, err := d.EnsureClient()
	if err != nil {
		return err
	}

	s := status.New(c)
	taskID, err := s.VmShutdown(context.Background(), status.VmShutdownRequest{
		Node: d.Node,
		Vmid: d.VMID,
	})
	if err != nil {
		return err
	}

	return d.waitForTaskToComplete(taskID, 10*time.Minute)
}

// Restart restarts the VM
func (d *Driver) Restart() error {
	if d.VMID < 1 {
		return errors.New("invalid VMID")
	}

	c, err := d.EnsureClient()
	if err != nil {
		return err
	}

	s := status.New(c)
	taskID, err := s.VmReboot(context.Background(), status.VmRebootRequest{
		Node: d.Node,
		Vmid: d.VMID,
	})
	if err != nil {
		return err
	}

	return d.waitForTaskToComplete(taskID, 10*time.Minute)
}

// Kill the VM immediately
func (d *Driver) Kill() error {
	if d.VMID < 1 {
		return errors.New("invalid VMID")
	}

	c, err := d.EnsureClient()
	if err != nil {
		return err
	}

	s := status.New(c)
	taskID, err := s.VmStop(context.Background(), status.VmStopRequest{
		Node: d.Node,
		Vmid: d.VMID,
	})
	if err != nil {
		return err
	}

	return d.waitForTaskToComplete(taskID, 10*time.Minute)
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

// Upgrade is currently a NOOP
func (d *Driver) Upgrade() error {
	return nil
}

// NewDriver returns a new driver
func NewDriver(hostName, storePath string) drivers.Driver {
	return &Driver{
		BaseDriver: &drivers.BaseDriver{
			SSHUser:     "docker",
			MachineName: hostName,
			StorePath:   storePath,
		},
	}
}
