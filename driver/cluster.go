package driver

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/FreekingDean/proxmox-api-go/proxmox/cluster/ha/groups"
	"github.com/FreekingDean/proxmox-api-go/proxmox/nodes"
	"github.com/FreekingDean/proxmox-api-go/proxmox/nodes/qemu"
)

const (
	_ = 1 << (iota * 10)
	KB
	MB
	GB
)

func (d *Driver) findAvailableNode() (string, error) {
	d.debugf("finding available node")
	client, err := d.EnsureClient()
	if err != nil {
		return "", err
	}

	if d.Node != "" {
		d.debugf("found node %s using that", d.Node)
		return d.Node, nil
	}
	if d.Group == "" {
		return "", fmt.Errorf("Cannot automatically choose node without HA group")
	}

	d.debugf("loading group %s", d.Group)
	g := groups.New(client)
	group, err := g.Find(context.Background(), groups.FindRequest{Group: d.Group})
	if err != nil {
		return "", err
	}
	nodesStr, ok := group["nodes"].(string)
	if !ok {
		return "", fmt.Errorf("bad format groups.nodes response %+v", group["nodes"])
	}
	d.debugf("looking for availability in %s", nodesStr)
	nodesList := strings.Split(strings.TrimSpace(nodesStr), ",")

	n := nodes.New(client)
	nodeResp, err := n.Index(context.Background())
	if err != nil {
		return "", err
	}

	q := qemu.New(client)

	// Look for lowest utilization based on memory
	bestNode := ""
	maxAvailMem := 0
	for _, node := range nodeResp {
		if !slices.Contains(nodesList, node.Node) {
			continue
		}
		if node.Status != "online" {
			continue
		}

		d.debugf("loading vms for %s", node.Node)
		vms, err := q.Index(context.Background(), qemu.IndexRequest{
			Node: node.Node,
		})
		if err != nil {
			return "", err
		}

		usedCPU := 0
		usedMem := 0
		for _, vm := range vms {
			if vm.Status != "running" {
				continue
			}
			usedCPU += int(*vm.Cpus)
			usedMem += *vm.Maxmem
		}
		d.debugf("Checking node with %dCPU & %dMemory", *node.Cpu, *node.Maxmem/GB)
		d.debugf("Using %dCPU & %dMemory", usedCPU, usedMem/GB)
		if *node.Maxmem-usedMem > maxAvailMem &&
			usedMem+d.Memory*GB < *node.Maxmem &&
			d.CPUCores+usedCPU < int(*node.Cpu) {
			bestNode = node.Node
			maxAvailMem = (*node.Maxmem) - usedMem
		}
	}
	if bestNode == "" {
		return "", fmt.Errorf("Could not find an available, online node for placement")
	}
	return bestNode, nil
}
