package driver

import (
	"context"
	"errors"
	"time"

	"github.com/FreekingDean/proxmox-api-go/proxmox/nodes/qemu/status"
	"github.com/docker/machine/libmachine/state"
)

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
	if err != nil {
		return state.Error, err
	}

	if resp.Status == status.Status_STOPPED {
		return state.Stopped, nil
	}
	if resp.Status == status.Status_RUNNING {
		return state.Running, nil
	}
	return state.Error, nil
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
