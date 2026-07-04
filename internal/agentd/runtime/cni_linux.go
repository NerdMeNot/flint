package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/containernetworking/cni/libcni"
)

// flintCNIConf is the network every step-with-services netns joins: a NAT'd
// bridge (outbound works, steps stay unreachable from outside) plus loopback
// (services and the step share the ns, so they talk over 127.0.0.1).
const flintCNIConf = `{
  "cniVersion": "1.0.0",
  "name": "flint",
  "plugins": [
    {
      "type": "bridge",
      "bridge": "flint0",
      "isGateway": true,
      "ipMasq": true,
      "ipam": {
        "type": "host-local",
        "ranges": [[{"subnet": "10.88.0.0/16"}]],
        "routes": [{"dst": "0.0.0.0/0"}]
      }
    },
    {"type": "loopback"}
  ]
}`

// cniManager lazily wires libcni against the bundled plugin binaries. Only
// steps with service containers pay for any of this — service-less steps stay
// on host networking with zero CNI dependency.
type cniManager struct {
	bundleDir string

	once sync.Once
	err  error
	cni  libcni.CNI
	conf *libcni.NetworkConfigList
}

func (m *cniManager) init() {
	m.once.Do(func() {
		binDirs := []string{filepath.Join(m.bundleDir, "cni"), "/opt/cni/bin"}
		found := false
		for _, d := range binDirs {
			if _, err := os.Stat(filepath.Join(d, "bridge")); err == nil {
				found = true
				break
			}
		}
		if !found {
			m.err = fmt.Errorf("cni plugins not found in %v — re-run scripts/bundle-runtime.sh (services need the bridge/host-local/loopback plugins)", binDirs)
			return
		}
		conf, err := libcni.ConfListFromBytes([]byte(flintCNIConf))
		if err != nil {
			m.err = fmt.Errorf("cni: parse config: %w", err)
			return
		}
		m.conf = conf
		m.cni = libcni.NewCNIConfig(binDirs, nil)
	})
}

// Setup attaches a pinned netns to the flint bridge.
func (m *cniManager) Setup(ctx context.Context, id, nsPath string) error {
	m.init()
	if m.err != nil {
		return m.err
	}
	_, err := m.cni.AddNetworkList(ctx, m.conf, &libcni.RuntimeConf{
		ContainerID: id, NetNS: nsPath, IfName: "eth0",
	})
	if err != nil {
		return fmt.Errorf("cni: add network: %w", err)
	}
	return nil
}

// Teardown releases the netns's bridge attachment and IPAM lease.
func (m *cniManager) Teardown(ctx context.Context, id, nsPath string) {
	m.init()
	if m.err != nil {
		return
	}
	_ = m.cni.DelNetworkList(ctx, m.conf, &libcni.RuntimeConf{
		ContainerID: id, NetNS: nsPath, IfName: "eth0",
	})
}
