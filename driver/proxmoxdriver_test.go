package driver

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FreekingDean/proxmox-api-go/proxmox"
	"github.com/stretchr/testify/assert"
)

func TestCheckIP(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, mockresp)
	}))
	d := &Driver{
		driverDebug: true,
		client:      proxmox.NewClient(s.URL),
	}
	resp, err := d.checkIP()
	assert.NoError(t, err)
	assert.Equal(t, "10.0.0.1", resp)
}

const mockresp = `
{ "data":{
  "result": [
    {
      "hardware-address": "00:00:00:00:00:00",
      "ip-addresses": [
        {
          "ip-address": "127.0.0.1",
          "ip-address-type": "ipv4",
          "prefix": 8
        },
        {
          "ip-address": "::1",
          "ip-address-type": "ipv6",
          "prefix": 128
        }
      ],
      "name": "lo",
      "statistics": {
        "rx-bytes": 0,
        "rx-dropped": 0,
        "rx-errs": 0,
        "rx-packets": 0,
        "tx-bytes": 0,
        "tx-dropped": 0,
        "tx-errs": 0,
        "tx-packets": 6
      }
    },
    {
      "hardware-address": "12:34:56:67:9a:bc",
      "ip-addresses": [
        {
          "ip-address": "10.0.0.1",
          "ip-address-type": "ipv4",
          "prefix": 16
        },
        {
          "ip-address": "abcd::1234",
          "ip-address-type": "ipv6",
          "prefix": 64
        }
      ],
      "name": "ens18",
      "statistics": {
        "rx-bytes": 0,
        "rx-dropped": 0,
        "rx-errs": 0,
        "rx-packets": 0,
        "tx-bytes": 0,
        "tx-dropped": 0,
        "tx-errs": 0,
        "tx-packets": 0
      }
    }
  ]
}}`
