package driver

import (
	"fmt"

	"github.com/docker/machine/libmachine/drivers"

	"github.com/FreekingDean/proxmox-api-go/proxmox"
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
	Scsi         string //Scsi0 data
	ScsiImport   string //Scsi0 Import
	ScsiDiskSize int    //Scsi1 DiskSize

	Memory   int // memory in GB
	CPUCores int // The number of cores per socket.

	NetBridge  string // bridge applied to network interface
	NetVlanTag int    // vlan tag

	SSHImportID string // SSH Import ID Keys

	VMID        int  // (generated) Proxmox VM ID
	driverDebug bool // driver debugging
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

func (d *Driver) DriverName() string {
	return "proxmoxve"
}

// SetConfigFromFlags configures all command line arguments
func (d *Driver) SetConfigFromFlags(flags drivers.DriverOptions) error {
	// PROXMOX API Connection settings
	d.Host = flags.String(flagProxmoxHost)
	d.Node = flags.String(flagProxmoxNode)
	d.User = flags.String(flagProxmoxUserName)
	d.Password = flags.String(flagProxmoxUserPassword)
	d.Realm = flags.String(flagProxmoxRealm)
	d.Pool = flags.String(flagProxmoxPool)

	// VM configuration
	d.Memory = flags.Int(flagVMMemory)
	d.Memory *= 1024
	d.CPUCores = flags.Int(flagVMCores)
	d.Scsi = flags.String(flagVMSCSIFilename)
	d.ScsiImport = flags.String(flagVMSCSIImport)
	d.ScsiDiskSize = flags.Int(flagVMSCSISize)
	d.NetBridge = flags.String(flagVMNetBridge)
	d.NetVlanTag = flags.Int(flagVMNetTag)

	d.SSHUser = flags.String(flagSSHUsername)
	d.SSHImportID = flags.String(flagSSHImportID)
	d.SSHPort = flags.Int(flagSSHPort)

	//Debug option
	d.driverDebug = flags.Bool(flagDebug)

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

// GetSSHHostname returns the ssh host returned by the API
func (d *Driver) GetSSHHostname() (string, error) {
	return d.GetIP()
}

// Upgrade is currently a NOOP
func (d *Driver) Upgrade() error {
	return nil
}
