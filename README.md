# BoxEngine

A simple container engine written in go.

```bash
$ ./BoxEngine
BoxEngine version: v0.0.1
Usage: ./BoxEngine [OPTIONS] run [CMD]

Options:
  -bridgeAddr string
        IP address (CIDR) for bridge (default "192.168.0.1/24")
  -bridgeName string
        NIC name for bridge (default "br0")
  -netsetgoPath string
        Path to the netsetgo binary (default "/usr/local/bin/netsetgo")
  -rootfsPath string
        Path to the root filesystem (default "./rootfs")
  -vethAddr string
        IP address (CIDR) for veth (default "192.168.0.10/24")
  -vethName string
        NIC name for veth (default "veth0")
```

