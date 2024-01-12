package main

import (
	"github.com/FreekingDean/docker-machine-driver-proxmox-ve/driver"
	"github.com/docker/machine/libmachine/drivers/plugin"
)

func main() {
	plugin.RegisterDriver(driver.NewDriver("default", ""))
}
