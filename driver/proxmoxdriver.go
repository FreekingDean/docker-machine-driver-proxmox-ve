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
	"github.com/FreekingDean/proxmox-api-go/proxmox/nodes/storage"
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
	ImageFile   string // in the format <storagename>:iso/<filename>.iso
	ISOUrl      string
	ISOFilename string

	Storage  string // internal PVE storage name
	DiskSize int    // disk size in GB
	Memory   int    // memory in GB

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
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_VM_STORAGE_PATH",
			Name:   "proxmoxve-vm-storage-path",
			Usage:  "storage to create the VM volume on",
			Value:  "", // leave the flag default value blank to support the clone default behavior if not explicity set of 'use what is most appropriate'
		},
		mcnflag.IntFlag{
			EnvVar: "PROXMOXVE_VM_STORAGE_SIZE",
			Name:   "proxmoxve-vm-storage-size",
			Usage:  "disk size in GB",
			Value:  16,
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
			EnvVar: "PROXMOXVE_ISO_URL",
			Name:   "proxmoxve-vm-iso-url",
			Usage:  "ISO Download URL",
			Value:  "",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_VM_ISO_FILENAME",
			Name:   "proxmoxve-vm-iso-filename",
			Usage:  "name of iso file post download",
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

	d.ProvisionStrategy = flags.String("proxmoxve-provision-strategy")

	// PROXMOX API Connection settings
	d.Host = flags.String("proxmoxve-proxmox-host")
	d.Node = flags.String("proxmoxve-proxmox-node")

	d.User = flags.String("proxmoxve-proxmox-user-name")
	d.Password = flags.String("proxmoxve-proxmox-user-password")
	d.Realm = flags.String("proxmoxve-proxmox-realm")
	d.Pool = flags.String("proxmoxve-proxmox-pool")

	// VM configuration
	d.DiskSize = flags.Int("proxmoxve-vm-storage-size")
	d.Storage = flags.String("proxmoxve-vm-storage-path")
	d.Memory = flags.Int("proxmoxve-vm-memory")
	d.Memory *= 1024
	d.GuestUsername = "docker"
	d.ISOUrl = flags.String("proxmoxve-vm-iso-url")
	d.ISOFilename = flags.String("proxmoxve-vm-iso-filename")
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

	if len(d.Storage) < 1 {
		d.Storage = "local"
	}

	if len(d.NetBridge) < 1 {
		d.NetBridge = "vmbr0"
	}

	return nil
}

// Create creates a new VM with storage
func (d *Driver) Create() error {
	c, err := d.EnsureClient()
	if err != nil {
		return err
	}

	cclient := cluster.New(c)
	id, err := cclient.Nextid(context.Background(), cluster.NextidRequest{})
	if err != nil {
		return err
	}
	tvalue := true

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

	s := storage.New(c)
	taskID, err := s.DownloadUrl(context.Background(), storage.DownloadUrlRequest{
		Content:  "iso",
		Filename: d.ISOFilename,
		Storage:  d.Storage,
		Node:     d.Node,
		Url:      d.ISOUrl,
	})
	err = d.waitForTaskToComplete(taskID, 10*time.Minute)
	if err != nil {
		return err
	}

	d.debugf("Next ID is '%s'", id)
	d.VMID = id
	req := qemu.CreateRequest{
		Vmid:   d.VMID,
		Args:   proxmox.String(fmt.Sprintf("-fw_cfg %s", strings.Replace(string(cfgStr), ",", ",,", -1))),
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
		Ides: &qemu.Ides{
			&qemu.Ide{
				File:  fmt.Sprintf("%s:iso/%s", d.Storage, d.ISOFilename),
				Media: qemu.PtrIdeMedia(qemu.IdeMedia_CDROM),
			},
		},
		Scsis: &qemu.Scsis{
			&qemu.Scsi{
				File: fmt.Sprintf("%s:%d", d.Storage, d.DiskSize),
			},
		},
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
		return "", fmt.Errorf("Bad response type")
	}
	for _, nic := range data["result"] {
		name, ok := nic["name"].(string)
		if !ok {
			return "", fmt.Errorf("Bad response type")
		}
		if name != "lo" {
			ips, ok := nic["ip-addresses"].([]map[string]string)
			if !ok {
				return "", fmt.Errorf("Bad response type")
			}
			for _, ip := range ips {
				if ip["ip-address-type"] == "ipv4" && ip["ip-address"] != "127.0.0.1" {
					return ip["ip-address"], nil
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
			if resp.Status != "OK" {
				return fmt.Errorf("task failed")
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
		return err
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
