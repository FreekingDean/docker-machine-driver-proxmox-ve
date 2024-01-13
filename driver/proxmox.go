package driver

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/FreekingDean/proxmox-api-go/proxmox"
	"github.com/FreekingDean/proxmox-api-go/proxmox/access"
	"github.com/FreekingDean/proxmox-api-go/proxmox/nodes/qemu/agent"
	"github.com/FreekingDean/proxmox-api-go/proxmox/nodes/tasks"
)

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

type AgentResponse struct {
	Result []struct {
		Name        string `json:"name"`
		IPAddresses []struct {
			IPAddressType string `json:"ip-address-type"`
			IPAddress     string `json:"ip-address"`
		} `json:"ip-addresses"`
	} `json:"result"`
}

func (d *Driver) getVMIp() (string, error) {
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
	if err != nil {
		d.debugf("error getting agent: %w", err)
		return "", nil
	}

	jsonStr, err := json.Marshal(resp)
	d.debugf("agent-resp: %s", jsonStr)
	if err != nil {
		d.debugf("Error marshalling json: %w", err)
		return "", nil
	}

	data := &AgentResponse{}
	err = json.Unmarshal(jsonStr, data)
	if err != nil {
		d.debugf("Error unmarshalling json: %w", err)
		return "", nil
	}

	for _, nic := range data.Result {
		if nic.Name != "lo" {
			for _, ip := range nic.IPAddresses {
				if ip.IPAddressType == "ipv4" && ip.IPAddress != "127.0.0.1" {
					return ip.IPAddress, nil
				}
			}
		}
	}
	return "", nil
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
