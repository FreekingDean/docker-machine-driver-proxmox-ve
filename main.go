package main

import (
	"github.com/FreekingDean/docker-machine-driver-proxmoxve/driver"
	"github.com/docker/machine/libmachine/drivers/plugin"
)

func main() {
	plugin.RegisterDriver(driver.NewDriver("default", ""))
}
