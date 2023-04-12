package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/SynologyOpenSource/synology-csi/pkg/dsm/webapi"
	"github.com/pfrybar/syno-iscsi/syno"
	"github.com/urfave/cli/v2"
	"golang.org/x/term"
)

var (
	out        io.Writer   = os.Stdout
	in         io.Reader   = os.Stdin
	synoClient syno.Client = &syno.DSMClient{}

	// password should be masked, which the 'term' library handles
	// can't use the global 'in' io.Reader since it wouldn't be masked
	stdin = int(syscall.Stdin)

	host  string
	port  int
	user  string
	pass  string
	https bool

	lunRegex = regexp.MustCompile("^[a-zA-Z0-9-]+$")
)

const (
	gb = 1024 * 1024 * 1024

	defaultPort = 5000
	hostEnvVar  = "SYNO_HOST"
	portEnvVar  = "SYNO_PORT"
	userEnvVar  = "SYNO_USER"
	passEnvVar  = "SYNO_PASS"
	httpsEnvVar = "SYNO_HTTPS"

	missingGlobalArgsMsg     = "the following global flag(s) are missing: %s"
	notEnoughArgsMsg         = "invalid number of arguments, expected %d but got %d"
	lunReclaimThinMsg        = "--reclaim can only be used with --thin"
	lunInvalidNameMsg        = "invalid LUN name, must consist of a-z, A-Z, 0-9, and hyphens (-)"
	lunInvalidSizeMsg        = "invalid LUN size, must be a positive integer"
	volumeNotEnoughSpaceMsg  = "not enough space, %s has %d GiB free"
	lunCannotDecreaseSizeMsg = "LUN cannot decrease in size"
	targetActiveSessionMsg   = "There are active sessions, please logout of all clients before continuing (force delete with -f)"
	targetForceDeleteMsg     = "Force deleting even though there are active sessions"

	volumeNotFoundMsg = "could not find volume with path: %s"
	lunNotFoundMsg    = "could not find LUN with name: %s"
	targetNotFoundMsg = "could not find target with name: %s"

	lunCreatedMsg    = "LUN created successfully"
	lunMappedMsg     = "LUN mapped to the target successfully"
	lunResizedMsg    = "LUN resized successfully"
	lunClonedMsg     = "LUN cloned successfully"
	lunDeletedMsg    = "LUN deleted successfully"
	targetCreatedMsg = "Target created successfully"
	targetDeletedMsg = "Target deleted successfully"
)

type errApp struct {
	s string
}

func (e *errApp) Error() string {
	return e.s
}

func main() {
	if err := app.Run(os.Args); err != nil {
		var errApp *errApp
		if errors.As(err, &errApp) {
			fmt.Fprintf(out, "Error: %s\n", errApp.Error())
		} else {
			fmt.Fprintf(out, "Unknown error: %s\n", err.Error())
		}
		os.Exit(1)
	}
}

var app = &cli.App{
	Name:                   "syno-iscsi",
	Version:                "v0.2.0",
	Usage:                  "CLI for interacting with Synology iSCSI storage",
	Writer:                 out,
	UseShortOptionHandling: true,
	Suggest:                true,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:        "host",
			Usage:       "synology host or ip",
			Destination: &host,
			EnvVars:     []string{hostEnvVar},
		},
		&cli.IntFlag{
			Name:        "port",
			Usage:       "port on which synology DSM is listening",
			Destination: &port,
			EnvVars:     []string{portEnvVar},
			Value:       defaultPort,
		},
		&cli.StringFlag{
			Name:        "user",
			Usage:       "synology user",
			Destination: &user,
			EnvVars:     []string{userEnvVar},
		},
		&cli.StringFlag{
			Name:        "pass",
			Usage:       "synology password",
			Destination: &pass,
			EnvVars:     []string{passEnvVar},
		},
		&cli.BoolFlag{
			Name:        "https",
			Usage:       "use https for connection to synology DSM",
			Destination: &https,
			EnvVars:     []string{httpsEnvVar},
		},
	},
	Commands: []*cli.Command{
		{
			Name:  "volume",
			Usage: "Volume management (list)",
			Subcommands: []*cli.Command{
				&volumeListCmd,
			},
		},
		{
			Name:  "lun",
			Usage: "LUN management (list, create, map, resize, clone, delete)",
			Subcommands: []*cli.Command{
				&lunListCmd, &lunCreateCmd, &lunMapCmd, &lunResizeCmd, &lunCloneCmd, &lunDeleteCmd,
			},
		},
		{
			Name:  "target",
			Usage: "Target management (list, create, delete)",
			Subcommands: []*cli.Command{
				&targetListCmd, &targetCreateCmd, &targetDeleteCmd,
			},
		},
	},
}

var volumeListCmd = cli.Command{
	Name:      "list",
	Usage:     "list volumes",
	ArgsUsage: " ",
	Action: func(ctx *cli.Context) error {
		if err := verifyArgs(0, ctx); err != nil {
			return err
		}

		if err := initAndLogin(ctx); err != nil {
			return err
		}
		defer logout()

		volumes, err := synoClient.VolumeList()
		if err != nil {
			return err
		}

		writer := new(tabwriter.Writer)
		writer.Init(out, 8, 8, 2, ' ', 0)
		defer writer.Flush()

		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n", "PATH", "STATUS", "FILESYSTEM", "SIZE", "USED")
		for _, volume := range volumes {
			size, err1 := strconv.ParseUint(volume.Size, 10, 64)
			free, err2 := strconv.ParseUint(volume.Free, 10, 64)

			readableSize := "?"
			readableUsed := "?"
			if err1 == nil && err2 == nil {
				readableSize = readableByteSize(size)
				readableUsed = readableByteSize(size - free)
			}

			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n", volume.Path, volume.Status, volume.FsType, readableSize, readableUsed)
		}

		return nil
	},
}

// TODO: list luns for a particular volume or target
var lunListCmd = cli.Command{
	Name:      "list",
	Usage:     "list LUNs",
	ArgsUsage: " ",
	Action: func(ctx *cli.Context) error {
		if err := verifyArgs(0, ctx); err != nil {
			return err
		}

		if err := initAndLogin(ctx); err != nil {
			return err
		}
		defer logout()

		luns, err := synoClient.LunList()
		if err != nil {
			return err
		}

		writer := new(tabwriter.Writer)
		writer.Init(out, 8, 8, 2, ' ', 0)
		defer writer.Flush()

		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n", "NAME", "VOLUME", "STATUS", "SIZE", "USED", "THIN")
		for _, lun := range luns {
			readableSize := readableByteSize(lun.Size)
			readableUsed := readableByteSize(lun.Used)

			var thin string
			if syno.IsThin(lun.LunType) {
				thin = "yes"
			} else {
				thin = "no"
			}

			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n", lun.Name, lun.Location, lun.Status, readableSize, readableUsed, thin)
		}

		return nil
	},
}

// TODO: can't set direct vs buffered i/o (thick), no option in webapi.DSM
var lunCreateCmd = cli.Command{
	Name:  "create",
	Usage: "create a LUN",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "thin",
			Aliases: []string{"t"},
			Usage:   "use thin provisioning",
		},
		&cli.BoolFlag{
			Name:    "reclaim",
			Aliases: []string{"r"},
			Usage:   "enable space reclamation (thin provisioning only)",
		},
		&cli.BoolFlag{
			Name:    "sync-cache",
			Aliases: []string{"s"},
			Usage:   "enable FUA and Sync Cache commands, recommended for SSDs",
		},
	},
	ArgsUsage: "<name> <volume> <size-in-gb>",
	Action: func(ctx *cli.Context) error {
		if err := verifyArgs(3, ctx); err != nil {
			return err
		}

		thin := ctx.Bool("thin")
		reclaim := ctx.Bool("reclaim")
		syncCache := ctx.Bool("sync-cache")

		if reclaim && !thin {
			return &errApp{lunReclaimThinMsg}
		}

		name := ctx.Args().Get(0)
		volumePath := ctx.Args().Get(1)
		sizeStr := ctx.Args().Get(2)

		if !lunRegex.MatchString(name) {
			return &errApp{lunInvalidNameMsg}
		}

		sizeGB, err := strconv.Atoi(sizeStr)
		if err != nil || sizeGB <= 0 {
			return &errApp{lunInvalidSizeMsg}
		}
		size := uint64(sizeGB) * gb

		if err := initAndLogin(ctx); err != nil {
			return err
		}
		defer logout()

		volume, err := getVolumeByPath(ctx, volumePath)
		if err != nil {
			return err
		}

		free, err := strconv.ParseUint(volume.Free, 10, 64)
		if err != nil {
			return err
		}

		if size > free {
			message := fmt.Sprintf(volumeNotEnoughSpaceMsg, volumePath, bytesToGiB(free))
			return &errApp{message}
		}

		devAttributes := []webapi.LunDevAttrib{}

		if reclaim {
			devAttributes = append(devAttributes, syno.LUN_SPACE_RECLAMATION)
		}

		if syncCache {
			devAttributes = append(devAttributes, syno.LUN_FUA_WRITE, syno.LUN_SYNC_CACHE)
		}

		lunType := syno.GetLunType(volume.FsType, thin)
		spec := webapi.LunCreateSpec{
			Name:       name,
			Location:   volumePath,
			Size:       int64(size),
			Type:       lunType,
			DevAttribs: devAttributes,
		}

		_, err = synoClient.LunCreate(spec)
		if err != nil {
			return err
		}

		fmt.Fprintln(out, lunCreatedMsg)

		return nil
	},
}

// TODO: can't have unmap lun command, no method in webapi.DSM
var lunMapCmd = cli.Command{
	Name:      "map",
	Usage:     "map a LUN to a target",
	ArgsUsage: "<lun-name> <target-name>",
	Action: func(ctx *cli.Context) error {
		if err := verifyArgs(2, ctx); err != nil {
			return err
		}

		lunName := ctx.Args().Get(0)
		targetName := ctx.Args().Get(1)

		if err := initAndLogin(ctx); err != nil {
			return err
		}
		defer logout()

		lun, err := getLunByName(ctx, lunName)
		if err != nil {
			return err
		}

		target, err := getTargetByName(ctx, targetName)
		if err != nil {
			return err
		}

		targetId := strconv.Itoa(target.TargetId)
		if err := synoClient.LunMapTarget([]string{targetId}, lun.Uuid); err != nil {
			return err
		}

		fmt.Fprintln(out, lunMappedMsg)

		return nil
	},
}

var lunResizeCmd = cli.Command{
	Name:      "resize",
	Usage:     "resize LUN by name (can only be increased)",
	ArgsUsage: "<name> <new-size-in-gb>",
	Action: func(ctx *cli.Context) error {
		if err := verifyArgs(2, ctx); err != nil {
			return err
		}

		name := ctx.Args().Get(0)
		sizeStr := ctx.Args().Get(1)

		sizeGB, err := strconv.Atoi(sizeStr)
		if err != nil || sizeGB <= 0 {
			return &errApp{lunInvalidSizeMsg}
		}
		size := uint64(sizeGB) * gb

		if err := initAndLogin(ctx); err != nil {
			return err
		}
		defer logout()

		lun, err := getLunByName(ctx, name)
		if err != nil {
			return err
		}

		if size <= lun.Size {
			return &errApp{lunCannotDecreaseSizeMsg}
		}

		volume, err := getVolumeByPath(ctx, lun.Location)
		if err != nil {
			return err
		}

		free, err := strconv.ParseUint(volume.Free, 10, 64)
		if err != nil {
			return err
		}

		if (size - lun.Size) > free {
			message := fmt.Sprintf(volumeNotEnoughSpaceMsg, lun.Location, bytesToGiB(free))
			return &errApp{message}
		}

		spec := webapi.LunUpdateSpec{
			Uuid:    lun.Uuid,
			NewSize: size,
		}

		if err := synoClient.LunUpdate(spec); err != nil {
			return err
		}

		fmt.Fprintln(out, lunResizedMsg)

		return nil
	},
}

var lunCloneCmd = cli.Command{
	Name:      "clone",
	Usage:     "clone a LUN",
	ArgsUsage: "<source-lun> <destination-lun> <volume>",
	Action: func(ctx *cli.Context) error {
		if err := verifyArgs(3, ctx); err != nil {
			return err
		}

		srcLunName := ctx.Args().Get(0)
		dstLunName := ctx.Args().Get(1)
		volumePath := ctx.Args().Get(2)

		if !lunRegex.MatchString(dstLunName) {
			return &errApp{lunInvalidNameMsg}
		}

		if err := initAndLogin(ctx); err != nil {
			return err
		}
		defer logout()

		srcLun, err := getLunByName(ctx, srcLunName)
		if err != nil {
			return err
		}

		volume, err := getVolumeByPath(ctx, volumePath)
		if err != nil {
			return err
		}

		free, err := strconv.ParseUint(volume.Free, 10, 64)
		if err != nil {
			return err
		}

		if srcLun.Size > free {
			message := fmt.Sprintf(volumeNotEnoughSpaceMsg, volumePath, bytesToGiB(free))
			return &errApp{message}
		}

		spec := webapi.LunCloneSpec{
			Name:       dstLunName,
			SrcLunUuid: srcLun.Uuid,
			Location:   volumePath,
		}

		_, err = synoClient.LunClone(spec)
		if err != nil {
			return err
		}

		fmt.Fprintln(out, lunClonedMsg)

		return nil
	},
}

var lunDeleteCmd = cli.Command{
	Name:  "delete",
	Usage: "delete LUN by name",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "skip-verify",
			Aliases: []string{"s"},
			Usage:   "skip verification",
		},
	},
	ArgsUsage: "<name>",
	Action: func(ctx *cli.Context) error {
		if err := verifyArgs(1, ctx); err != nil {
			return err
		}

		skip := ctx.Bool("skip-verify")

		name := ctx.Args().Get(0)

		if err := initAndLogin(ctx); err != nil {
			return err
		}
		defer logout()

		lun, err := getLunByName(ctx, name)
		if err != nil {
			return err
		}

		if !skip {
			targets, err := synoClient.TargetList()
			if err != nil {
				return err
			}

			// get targets mapped to this lun
			var mappedTargets []string
			for _, target := range targets {
				for _, mappedLun := range target.MappedLuns {
					if lun.Uuid == mappedLun.LunUuid {
						if len(target.ConnectedSessions) > 0 {
							mappedTargets = append(mappedTargets, target.Name+" (connected)")
						} else {
							mappedTargets = append(mappedTargets, target.Name)
						}
						break
					}
				}
			}

			fmt.Fprintln(out, "Are you sure you want to delete this lun?")

			if len(mappedTargets) > 0 {
				fmt.Fprintf(out, "It is mapped to the targets: %s\n", strings.Join(mappedTargets, ", "))
			}

			fmt.Fprintf(out, "Enter the lun name (%s) to continue: ", name)

			verify := scanLine()
			if name != verify {
				fmt.Fprintln(out, "Cancelled")
				return nil
			}
		}

		if err := synoClient.LunDelete(lun.Uuid); err != nil {
			return err
		}

		fmt.Fprintln(out, lunDeletedMsg)

		return nil
	},
}

var targetListCmd = cli.Command{
	Name:      "list",
	Usage:     "list targets",
	ArgsUsage: " ",
	Action: func(ctx *cli.Context) error {
		if err := verifyArgs(0, ctx); err != nil {
			return err
		}

		if err := initAndLogin(ctx); err != nil {
			return err
		}
		defer logout()

		targets, err := synoClient.TargetList()
		if err != nil {
			return err
		}

		luns, err := synoClient.LunList()
		if err != nil {
			return err
		}

		writer := new(tabwriter.Writer)
		writer.Init(out, 8, 8, 2, ' ', 0)
		defer writer.Flush()

		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", "NAME", "IQN", "SESSIONS", "LUNS")
		for _, target := range targets {
			lunString := buildLunString(luns, target.MappedLuns)
			sessions := fmt.Sprintf("%d/%d", len(target.ConnectedSessions), target.MaxSessions)
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", target.Name, target.Iqn, sessions, lunString)
		}

		return nil
	},
}

// TODO: validate IQN (e.g. must be < 128 characters)
var targetCreateCmd = cli.Command{
	Name:      "create",
	Usage:     "create a target",
	ArgsUsage: "<name> <iqn>",
	Action: func(ctx *cli.Context) error {
		if err := verifyArgs(2, ctx); err != nil {
			return err
		}

		name := ctx.Args().Get(0)
		iqn := ctx.Args().Get(1)

		if err := initAndLogin(ctx); err != nil {
			return err
		}
		defer logout()

		spec := webapi.TargetCreateSpec{
			Name: name,
			Iqn:  iqn,
		}

		_, err := synoClient.TargetCreate(spec)
		if err != nil {
			return err
		}

		fmt.Fprintln(out, targetCreatedMsg)

		return nil
	},
}

var targetDeleteCmd = cli.Command{
	Name:  "delete",
	Usage: "delete target by name",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "force",
			Aliases: []string{"f"},
			Usage:   "force deletion",
		},
		&cli.BoolFlag{
			Name:    "skip-verify",
			Aliases: []string{"s"},
			Usage:   "skip verification",
		},
	},
	ArgsUsage: "<name>",
	Action: func(ctx *cli.Context) error {
		if err := verifyArgs(1, ctx); err != nil {
			return err
		}

		force := ctx.Bool("force")
		skip := ctx.Bool("skip-verify")

		name := ctx.Args().Get(0)

		if err := initAndLogin(ctx); err != nil {
			return err
		}
		defer logout()

		target, err := getTargetByName(ctx, name)
		if err != nil {
			return err
		}

		if len(target.ConnectedSessions) > 0 {
			if force {
				fmt.Fprintln(out, targetForceDeleteMsg)
			} else {
				fmt.Fprintln(out, targetActiveSessionMsg)
				return nil
			}
		}

		if !skip {
			fmt.Fprintln(out, "Are you sure you want to delete this target?")
			fmt.Fprintf(out, "Enter the target name (%s) to continue: ", name)

			verify := scanLine()
			if name != verify {
				fmt.Fprintln(out, "Cancelled")
				return nil
			}
		}

		if err := synoClient.TargetDelete(strconv.Itoa(target.TargetId)); err != nil {
			return err
		}

		fmt.Fprintln(out, targetDeletedMsg)

		return nil
	},
}

func verifyArgs(numArgs int, ctx *cli.Context) error {
	if ctx.NArg() != numArgs {
		message := fmt.Sprintf(notEnoughArgsMsg, numArgs, ctx.NArg())
		return &errApp{message}
	}

	return nil
}

func initAndLogin(ctx *cli.Context) error {
	// check for required global flags here instead of setting them to 'Required'
	// this allows users to explore the API (e.g. 'help' command) without these flags
	missing := []string{}
	if host == "" {
		missing = append(missing, "host")
	}
	if user == "" {
		missing = append(missing, "user")
	}
	if pass == "" && !term.IsTerminal(stdin) {
		missing = append(missing, "pass")
	}

	if len(missing) > 0 {
		return &errApp{fmt.Sprintf(missingGlobalArgsMsg, strings.Join(missing, ", "))}
	}

	if pass == "" {
		if err := readPass(&pass); err != nil {
			return err
		}
	}

	synoClient.Init(host, port, user, pass, https)

	if err := synoClient.Login(); err != nil {
		// webapi.DSM() does not expose errors so have to manually parse the error string
		if err.Error() == "DSM Api error. Error code:400" {
			return &errApp{"Invalid user and/or pass"}
		}
		if strings.Contains(err.Error(), "dial tcp") {
			// most likely a problem connecting to host
			return &errApp{fmt.Sprintf("problem connecting to host (%s)", err.Error())}
		}

		return err
	}

	return nil
}

func logout() {
	if err := synoClient.Logout(); err != nil {
		fmt.Fprintf(out, "Error: failed to logout of DSM: %s", err.Error())
	}
}

func readPass(pass *string) error {
	fmt.Fprint(out, "Enter Password: ")
	passBytes, err := term.ReadPassword(stdin)
	if err != nil {
		return err
	}
	fmt.Fprintln(out)

	*pass = string(passBytes)
	return nil
}

func readableByteSize(size uint64) string {
	fsize := float64(size)
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	if size < 1 {
		return "0.00 B"
	}
	exp := int(math.Log(fsize) / math.Log(1024))
	val := fsize / math.Pow(1024, float64(exp))
	return fmt.Sprintf("%.2f %s", val, units[exp])
}

func bytesToGiB(size uint64) int {
	return int(size / gb)
}

func buildLunString(luns []webapi.LunInfo, mappedLuns []webapi.MappedLun) string {
	var found []string
	for _, mapped := range mappedLuns {
		for _, lun := range luns {
			if mapped.LunUuid == lun.Uuid {
				found = append(found, lun.Name)
			}
		}
	}

	return strings.Join(found, ",")
}

func getVolumeByPath(ctx *cli.Context, path string) (*webapi.VolInfo, error) {
	volumes, err := synoClient.VolumeList()
	if err != nil {
		return nil, err
	}

	for _, volume := range volumes {
		if path == volume.Path {
			return &volume, nil
		}
	}

	return nil, &errApp{fmt.Sprintf(volumeNotFoundMsg, path)}
}

func getLunByName(ctx *cli.Context, name string) (*webapi.LunInfo, error) {
	luns, err := synoClient.LunList()
	if err != nil {
		return nil, err
	}

	for _, lun := range luns {
		if name == lun.Name {
			return &lun, nil
		}
	}

	return nil, &errApp{fmt.Sprintf(lunNotFoundMsg, name)}
}

func getTargetByName(ctx *cli.Context, name string) (*webapi.TargetInfo, error) {
	targets, err := synoClient.TargetList()
	if err != nil {
		return nil, err
	}

	for _, target := range targets {
		if name == target.Name {
			return &target, nil
		}
	}

	return nil, &errApp{fmt.Sprintf(targetNotFoundMsg, name)}
}

func scanLine() string {
	scanner := bufio.NewScanner(in)
	scanner.Scan()
	return scanner.Text()
}
