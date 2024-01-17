package driver

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FreekingDean/proxmox-api-go/proxmox"
	"github.com/stretchr/testify/assert"
)

func TestFindAvailableNode(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/cluster/ha/groups") {
			assert.Equal(t, "/cluster/ha/groups/some-group", r.URL.Path)
			fmt.Fprintf(w, `{"data":{"nodes":"node1,node2"}}`)
			return
		}

		gig := 1024 * 1024 * 1024

		if strings.Contains(r.URL.Path, "/qemu") {
			fmt.Fprintf(w, `{"data":[{"cpus": 10,"maxmem":%d,"status":"running"}]}`, 16*gig)
			return
		}

		if strings.Contains(r.URL.Path, "/nodes") {
			fmt.Fprintf(w, `{"data":[{"node":"node2","maxmem":%d,"cpu":128,"status":"offline"},{"node":"node1","maxmem":%d,"cpu":%d,"status":"online"}]}`, 256*gig, 128*gig, 32)
			return
		}

		fmt.Fprint(w, "")
	}))
	d := &Driver{
		driverDebug: true,
		Group:       "some-group",
		client:      proxmox.NewClient(s.URL),
	}
	resp, err := d.findAvailableNode()
	assert.Equal(t, "node1", resp)
	assert.NoError(t, err)
}
