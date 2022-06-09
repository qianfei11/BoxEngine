package main

import (
    "fmt"
    "io/ioutil"
    "os"
    "os/exec"
    "path/filepath"
    "syscall"
    "strconv"
    "net"
    "time"
    "flag"
    "log"
)

// https://www.socketloop.com/tutorials/golang-force-your-program-to-run-with-root-permissions
func checkRoot() {
    cmd := exec.Command("id", "-u")
    output, err := cmd.Output()
    if err != nil {
        log.Fatal(err)
    }

    i, err := strconv.Atoi(string(output[:len(output) - 1]))
    if err != nil {
        log.Fatal(err)
    }

    if (i != 0) {
        log.Fatal("This program must be run as root! (sudo)")
    }
}

func usage() {
    fmt.Fprintf(os.Stderr, `BoxEngine version: v0.0.1
Usage: %s [OPTIONS] run [CMD]

Options:    
`, os.Args[0])
    flag.PrintDefaults()
}

var vethName, vethAddr string
var bridgeName, bridgeAddr string
var netsetgoPath string
var rootfsPath string

// sudo go main.go run /bin/bash
// creation of namespaces needs `CAP_SYS_ADMIN`
func main() {
    checkRoot()

    flag.StringVar(&vethName, "vethName", "veth0", "NIC name for veth")
    flag.StringVar(&vethAddr, "vethAddr", "192.168.0.10/24", "IP address (CIDR) for veth")
    flag.StringVar(&bridgeName, "bridgeName", "br0", "NIC name for bridge")
    flag.StringVar(&bridgeAddr, "bridgeAddr", "192.168.0.1/24", "IP address (CIDR) for bridge")
    flag.StringVar(&netsetgoPath, "netsetgoPath", "/usr/local/bin/netsetgo", "Path to the netsetgo binary")
    flag.StringVar(&rootfsPath, "rootfsPath", "./rootfs", "Path to the root filesystem")

    flag.Usage = usage

    flag.Parse()

    // https://darjun.github.io/2020/01/10/godailylib/flag/
    if (flag.NArg() < 2) {
        usage()
        os.Exit(-1)
    }

    switch flag.Arg(0) {
    case "run":
        run(flag.Args()[1:]...)
    case "child":
        child(flag.Args()[1:]...)
    default:
        panic("wat??")
    }
}

// https://www.anquanke.com/post/id/246601#h2-6
func waitForNetwork() error {
    maxWait := time.Second * 3
    checkInterval := time.Second
    timeStarted := time.Now()

    for {
        interfaces, err := net.Interfaces()
        if err != nil {
            return err
        }

        // pretty basic check ...
        // > 1 as a lo device will already exist
        if len(interfaces) > 1 {
            return nil
        }

        if time.Since(timeStarted) > maxWait {
            return fmt.Errorf("Timeout after %s waiting for network", maxWait)
        }

        time.Sleep(checkInterval)
    }
}

func exitIfNetsetgoNotFound(netsetgoPath string) {
    if _, err := os.Stat(netsetgoPath); os.IsNotExist(err) {
        usefulErrorMsg := fmt.Sprintf(`
Unable to find the netsetgo binary at "%s".
netsetgo is an external binary used to configure networking.
You must download netsetgo, chown it to the root user and apply the setuid bit.
This can be done as follows:
  wget "https://github.com/teddyking/netsetgo/releases/download/0.0.1/netsetgo"
  sudo mv netsetgo /usr/local/bin/
  sudo chown root:root /usr/local/bin/netsetgo
  sudo chmod 4755 /usr/local/bin/netsetgo
`, netsetgoPath)

        fmt.Println(usefulErrorMsg)
        os.Exit(1)
    }
}

func exitIfRootfsNotFound(rootfsPath string) {
    if _, err := os.Stat(rootfsPath); os.IsNotExist(err) {
        usefulErrorMsg := fmt.Sprintf(`
"%s" does not exist.
Please create this directory and unpack a suitable root filesystem inside it.
An example rootfs, BusyBox, can be downloaded and unpacked as follows:
  wget "https://raw.githubusercontent.com/teddyking/ns-process/4.0/assets/busybox.tar"
  mkdir -p %s
  tar -C %s -xf busybox.tar
Or export from a docker container:
  docker run -it centos:7 /bin/bash
  docker export $container_id --output=rootfs.tar
  mkdir -p %s
  tar -C %s -xf rootfs.tar
`, rootfsPath, rootfsPath, rootfsPath)

        fmt.Println(usefulErrorMsg)
        os.Exit(1)
    }
}

// https://itnext.io/container-from-scratch-348838574160
func run(cmd_args ...string) {
    //fmt.Printf("rootfsPath = %v\n", rootfsPath)

    exitIfNetsetgoNotFound(netsetgoPath)
    exitIfRootfsNotFound(rootfsPath)

    fmt.Printf("Running %v as PID %d\n", cmd_args[0], os.Getpid())

    args := append([]string{"child"}, cmd_args[0:]...)
    cmd := exec.Command("/proc/self/exe", args...)
    cmd.Stdin = os.Stdin
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    cmd.SysProcAttr = &syscall.SysProcAttr{
        Cloneflags: syscall.CLONE_NEWUSER |
            syscall.CLONE_NEWUTS |
            syscall.CLONE_NEWIPC |
            syscall.CLONE_NEWPID |
            syscall.CLONE_NEWNS |
            syscall.CLONE_NEWNET,
        UidMappings: []syscall.SysProcIDMap{
            {
                ContainerID: 0,
                HostID: os.Getuid(),
                Size: 1,
            },
        },
        GidMappings: []syscall.SysProcIDMap{
            {
                ContainerID: 0,
                HostID: os.Getgid(),
                Size: 1,
            },
        },
    }

    // https://iximiuz.com/en/posts/container-networking-is-simple/
    must(cmd.Start())
    pid := fmt.Sprintf("%d", cmd.Process.Pid)
    netsetgoCmd := exec.Command(netsetgoPath,
        "-pid", pid,
        "-containerAddress", vethAddr,
        "-vethNamePrefix", vethName,
        "-bridgeAddress", bridgeAddr,
        "-bridgeName", bridgeName)
    must(netsetgoCmd.Run())
    must(cmd.Wait())
}

var defaultMountFlags = syscall.MS_NOEXEC | syscall.MS_NOSUID | syscall.MS_NODEV

func child(cmd_args ...string) {
    fmt.Printf("Running %v as PID %d\n", cmd_args[0], os.Getpid())

    pids_cg()
    memory_cg()
    cpu_cg()

    must(syscall.Sethostname([]byte("BoxEngine")))

    //fmt.Printf("rootfsPath = %v\n", rootfsPath)
    must(syscall.Chroot(rootfsPath))
    must(os.Chdir("/"))

    must(syscall.Mount("proc", "/proc", "proc", uintptr(defaultMountFlags), ""))
    must(syscall.Mount("sysfs", "/sys", "sysfs", uintptr(defaultMountFlags), ""))

    must(waitForNetwork())

    must(syscall.Exec(cmd_args[0], cmd_args, os.Environ()))

    // cleanup mount
    must(syscall.Unmount("proc", 0))
    must(syscall.Unmount("sys", 0))
}

// https://github.com/mugli/container-from-scratch-in-go
// https://github.com/lizrice/containers-from-scratch
func pids_cg() {
    cgPath := "/sys/fs/cgroup/"
    pids := filepath.Join(cgPath, "pids")
    boxEngine := filepath.Join(pids, "BoxEngine")
    os.Mkdir(boxEngine, 0755)

    must(ioutil.WriteFile(filepath.Join(boxEngine, "pids.max"), []byte("10"), 0700))
    must(ioutil.WriteFile(filepath.Join(boxEngine, "notify_on_release"), []byte("1"), 0700))
    pid := strconv.Itoa(os.Getpid())
    must(ioutil.WriteFile(filepath.Join(boxEngine, "cgroup.procs"), []byte(pid), 0700))
}

func memory_cg() {
    cgPath := "/sys/fs/cgroup/"
    memory := filepath.Join(cgPath, "memory")
    boxEngine := filepath.Join(memory, "BoxEngine")
    os.Mkdir(boxEngine, 0755)

    must(ioutil.WriteFile(filepath.Join(boxEngine, "memory.limit_in_bytes"), []byte("5M"), 0700))
    must(ioutil.WriteFile(filepath.Join(boxEngine, "memory.swappiness"), []byte("0"), 0700))
    must(ioutil.WriteFile(filepath.Join(boxEngine, "notify_on_release"), []byte("1"), 0700))
    pid := strconv.Itoa(os.Getpid())
    must(ioutil.WriteFile(filepath.Join(boxEngine, "cgroup.procs"), []byte(pid), 0700))
}

func cpu_cg() {
    cgPath := "/sys/fs/cgroup/"
    cpu := filepath.Join(cgPath, "cpu")
    boxEngine := filepath.Join(cpu, "BoxEngine")
    os.Mkdir(boxEngine, 0755)

    must(ioutil.WriteFile(filepath.Join(boxEngine, "cpu.cfs_period_us"), []byte("100000"), 0700))
    must(ioutil.WriteFile(filepath.Join(boxEngine, "cpu.cfs_quota_us"), []byte("50000"), 0700))
    must(ioutil.WriteFile(filepath.Join(boxEngine, "notify_on_release"), []byte("1"), 0700))
    pid := strconv.Itoa(os.Getpid())
    must(ioutil.WriteFile(filepath.Join(boxEngine, "cgroup.procs"), []byte(pid), 0700))
}

func must(err error) {
    if err != nil {
        panic(err)
    }
}
