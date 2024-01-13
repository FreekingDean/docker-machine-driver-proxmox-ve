package driver

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"strings"

	ignition "github.com/coreos/ignition/v2/config/v3_4/types"
	mcnssh "github.com/docker/machine/libmachine/ssh"
)

func (d *Driver) generateKey() (string, error) {
	// create and save a new SSH key pair
	d.debug("creating new ssh keypair")
	if err := mcnssh.GenerateSSHKey(d.GetSSHKeyPath()); err != nil {
		return "", fmt.Errorf("could not generate ssh key: %w", err)
	}
	buf, err := os.ReadFile(d.GetSSHKeyPath() + ".pub")
	if err != nil {
		return "", fmt.Errorf("could not read ssh public key: %w", err)
	}
	return string(buf), nil
}

func (d *Driver) importSSHKeys() ([]ignition.SSHAuthorizedKey, error) {
	keys := []ignition.SSHAuthorizedKey{}
	if d.SSHImportID == "" {
		return keys, nil
	}
	d.debugf("pulling github keys")

	parts := strings.Split(d.SSHImportID, ":")
	if len(parts) != 2 {
		return keys, fmt.Errorf("Invalid import id %s should look like gh:UserName", d.SSHImportID)
	}

	url := ""
	if parts[0] == "gh" {
		url = "https://github.com/%s.keys"
	} else if parts[0] == "lp" {
		url = "https://launchpad.net/~%s/+sshkeys"
	} else {
		return keys, fmt.Errorf("Invalid import type should be one of (gh, lp) got '%s'", parts[0])
	}

	resp, err := http.Get(fmt.Sprintf(url, parts[1]))
	if err != nil {
		return keys, err
	}
	if resp.StatusCode != 200 {
		return keys, fmt.Errorf("received non 200 from key import '%d'", resp.StatusCode)
	}

	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		keys = append(keys, ignition.SSHAuthorizedKey(strings.TrimSpace(scanner.Text())))
	}
	return keys, nil
}
