package driver

import (
	"strings"

	"github.com/docker/machine/libmachine/mcnflag"
)

const (
	flagProxmoxHost         = "proxmoxve-proxmox-host"
	flagProxmoxNode         = "proxmoxve-proxmox-node"
	flagProxmoxUserName     = "proxmoxve-proxmox-user-name"
	flagProxmoxUserPassword = "proxmoxve-proxmox-user-password"
	flagProxmoxRealm        = "proxmoxve-proxmox-realm"
	flagProxmoxPool         = "proxmoxve-proxmox-pool"

	flagVMMemory       = "proxmoxve-vm-memory"
	flagVMCores        = "proxmoxve-vm-cores"
	flagVMSCSIFilename = "proxmoxve-vm-scsi"
	flagVMSCSIImport   = "proxmoxve-vm-scsi-import"
	flagVMSCSISize     = "proxmoxve-vm-scsi-size"
	flagVMNetBridge    = "proxmoxve-vm-net-bridge"
	flagVMNetTag       = "proxmoxve-vm-net-tag"

	flagSSHUsername = "proxmoxve-ssh-username"
	flagSSHPort     = "proxmoxve-ssh-port"
	flagSSHImportID = "proxmoxve-ssh-import-id"
	flagDebug       = "proxmoxve-debug-driver"
)

// GetCreateFlags returns the argument flags for the program
func (d *Driver) GetCreateFlags() []mcnflag.Flag {
	return []mcnflag.Flag{
		stringFlag(flagProxmoxHost, "Host to connect to", "192.168.1.256"),
		stringFlag(flagProxmoxNode, "Node name to launch VMs on", ""),
		stringFlag(flagProxmoxUserName, "PVE API Username", "root"),
		stringFlag(flagProxmoxUserPassword, "PVE API Password", ""),
		stringFlag(flagProxmoxRealm, "PVE API Realm", "pam"),
		stringFlag(flagProxmoxPool, "Pool to attach to VMs", ""),

		intFlag(flagVMMemory, "VM Memory in GB", 8),
		intFlag(flagVMCores, "VM CPU Cores", 2),

		stringFlag(flagVMSCSIFilename, "VM SCSI0 Filename", ""),
		stringFlag(flagVMSCSIImport, "VM SCSI0 Disk to Import", ""),
		intFlag(flagVMSCSISize, "VM SCSI0 Disk Size GB", 32),

		stringFlag(flagVMNetBridge, "VM bridge network to attach", "vmbr0"),
		intFlag(flagVMNetTag, "VM VLAN Tag", 0),

		stringFlag(flagSSHUsername, "SSH Username", "rancher"),
		stringFlag(flagSSHImportID, "SSH Import ID (ie gh:GithubUsername)", ""),
		intFlag(flagSSHPort, "SSH Port", 22),
		boolFlag(flagDebug, "Debug driver"),
	}
}

func stringFlag(name string, desc string, value string) mcnflag.StringFlag {
	envName := strings.Replace(strings.ToUpper(name), "-", "_", -1)
	return mcnflag.StringFlag{
		EnvVar: envName,
		Name:   name,
		Usage:  desc,
		Value:  value,
	}
}

func intFlag(name string, desc string, value int) mcnflag.IntFlag {
	envName := strings.Replace(strings.ToUpper(name), "-", "_", -1)
	return mcnflag.IntFlag{
		EnvVar: envName,
		Name:   name,
		Usage:  desc,
		Value:  value,
	}
}

func boolFlag(name string, desc string) mcnflag.BoolFlag {
	envName := strings.Replace(strings.ToUpper(name), "-", "_", -1)
	return mcnflag.BoolFlag{
		EnvVar: envName,
		Name:   name,
		Usage:  desc,
	}
}
