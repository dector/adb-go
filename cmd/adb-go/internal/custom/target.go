package custom

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	adb "github.com/dector/adb-go"
	"github.com/dector/adb-go/internal/daemon"
)

type deviceClient interface {
	Close() error
	ShellStream(ctx context.Context, cmd string, stdout io.Writer) error
	Logcat(ctx context.Context, stdout io.Writer, opts adb.LogcatOptions) error
	GetProp(ctx context.Context, name string) (string, error)
	Properties(ctx context.Context) (map[string]string, error)
	Screencap(ctx context.Context) ([]byte, error)
	Reboot(ctx context.Context, mode adb.RebootMode) error
	PushFile(ctx context.Context, localPath, remotePath string) error
	PullFile(ctx context.Context, remotePath, localPath string) error
	PullFileWithOptions(ctx context.Context, remotePath, localPath string, opts adb.PullOptions) error
	InstallAPK(ctx context.Context, localPath string) error
	InstallAPKWithOptions(ctx context.Context, localPath string, opts adb.InstallOptions) error
}

type connectionOptions struct {
	addr      *string
	usb       *bool
	serial    *string
	usbPath   *string
	usbBus    *int
	usbDevice *int
	usbVID    *string
	usbPID    *string
	authKey   *string
}

type connectionTarget struct {
	description string
	tcpAddr     string
	usb         bool
	usbOptions  adb.USBOptions
	auth        []adb.AuthCredential
}

var connectDevice = func(ctx context.Context, target connectionTarget) (deviceClient, error) {
	if target.usb {
		return adb.ConnectUSB(ctx, target.usbOptions)
	}
	return adb.ConnectWithOptions(ctx, target.tcpAddr, adb.ConnectOptions{AuthCredentials: target.auth})
}

func connectTarget(ctx context.Context, command string, target connectionTarget, out outputPolicy) (deviceClient, error) {
	out.Verbosef("%s selected %s target %s%s\n", command, target.transportName(), target.description, target.authSummary())
	out.Verbosef("%s connecting to %s\n", command, target.description)
	client, err := connectDevice(ctx, target)
	if err != nil {
		return nil, err
	}
	out.Verbosef("%s connected to %s\n", command, target.description)
	return client, nil
}

func (t connectionTarget) transportName() string {
	if t.usb {
		return "USB"
	}
	return "TCP"
}

func (t connectionTarget) authSummary() string {
	if len(t.auth) == 0 {
		return ""
	}
	return fmt.Sprintf(" using %d auth credential(s)", len(t.auth))
}

var listUSBDevices = adb.ListUSBDevices
var scanTCPTargets = adb.ScanTCPTargets
var currentTime = time.Now
var sendDaemonRequest = daemon.Send

func addConnectionFlags(fs *flag.FlagSet) connectionOptions {
	return connectionOptions{
		addr:      fs.String("addr", "", "explicit TCP device address, for example 127.0.0.1:5555"),
		usb:       fs.Bool("usb", false, "connect over USB instead of TCP; Linux-only initially"),
		serial:    fs.String("serial", "", "USB serial selector; reserved for future string descriptor support"),
		usbPath:   fs.String("usb-path", "", "Linux usbfs device path, for example /dev/bus/usb/001/002"),
		usbBus:    fs.Int("usb-bus", 0, "Linux usbfs bus number for USB selection"),
		usbDevice: fs.Int("usb-device", 0, "Linux usbfs device number for USB selection"),
		usbVID:    fs.String("usb-vid", "", "USB vendor ID, for example 18d1 or 0x18d1"),
		usbPID:    fs.String("usb-pid", "", "USB product ID, for example 4ee7 or 0x4ee7"),
		authKey:   fs.String("auth-key", "", "explicit ADB RSA private key path for authenticated devices"),
	}
}

func (o connectionOptions) target(fs *flag.FlagSet) (connectionTarget, error) {
	authCredentials, err := o.authCredentials()
	if err != nil {
		return connectionTarget{}, err
	}

	addrProvided := flagWasProvided(fs, "addr")
	usbRequested := *o.usb || flagWasProvided(fs, "usb-path") || flagWasProvided(fs, "serial") || flagWasProvided(fs, "usb-bus") || flagWasProvided(fs, "usb-device") || flagWasProvided(fs, "usb-vid") || flagWasProvided(fs, "usb-pid")
	if addrProvided && usbRequested {
		return connectionTarget{}, fmt.Errorf("cannot combine TCP --addr with USB selection flags")
	}

	if usbRequested {
		opts := adb.USBOptions{
			DevicePath:   strings.TrimSpace(*o.usbPath),
			Serial:       strings.TrimSpace(*o.serial),
			BusNumber:    *o.usbBus,
			DeviceNumber: *o.usbDevice,
		}
		opts.AuthCredentials = authCredentials
		if opts.VendorID, err = parseUSBID("--usb-vid", *o.usbVID); err != nil {
			return connectionTarget{}, err
		}
		if opts.ProductID, err = parseUSBID("--usb-pid", *o.usbPID); err != nil {
			return connectionTarget{}, err
		}
		return connectionTarget{description: "USB device", usb: true, usbOptions: opts, auth: authCredentials}, nil
	}

	if addrProvided {
		addr := strings.TrimSpace(*o.addr)
		if addr == "" {
			return connectionTarget{}, fmt.Errorf("missing required --addr value")
		}
		return connectionTarget{description: addr, tcpAddr: addr, auth: authCredentials}, nil
	}

	addr := strings.TrimSpace(os.Getenv("ADB_GO_ADDR"))
	if addr != "" {
		return connectionTarget{description: addr, tcpAddr: addr, auth: authCredentials}, nil
	}
	return connectionTarget{}, fmt.Errorf("missing required --addr, ADB_GO_ADDR, or USB selection")
}

func (o connectionOptions) authCredentials() ([]adb.AuthCredential, error) {
	path := strings.TrimSpace(*o.authKey)
	if path == "" {
		return nil, nil
	}
	credential, err := adb.LoadPrivateKey(path)
	if err != nil {
		return nil, fmt.Errorf("load --auth-key: %w", err)
	}
	return []adb.AuthCredential{credential}, nil
}

func parseUSBID(name, value string) (uint16, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	base := 0
	if !strings.HasPrefix(value, "0x") && !strings.HasPrefix(value, "0X") {
		base = 16
	}
	id, err := strconv.ParseUint(value, base, 16)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q", name, value)
	}
	return uint16(id), nil
}

func flagWasProvided(fs *flag.FlagSet, name string) bool {
	provided := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			provided = true
		}
	})
	return provided
}
