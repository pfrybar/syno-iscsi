package syno

import "github.com/SynologyOpenSource/synology-csi/pkg/dsm/webapi"

// matches some of the methods from webapi.DSM
// Init is new, which allows the client to be initialised after creation
type Client interface {
	Init(host string, port int, user string, pass string, https bool)
	Login() error
	Logout() error
	VolumeList() ([]webapi.VolInfo, error)
	LunList() ([]webapi.LunInfo, error)
	LunCreate(spec webapi.LunCreateSpec) (string, error)
	LunDelete(lunUuid string) error
	LunMapTarget(targetIds []string, lunUuid string) error
	TargetList() ([]webapi.TargetInfo, error)
	TargetCreate(spec webapi.TargetCreateSpec) (string, error)
	TargetDelete(targetName string) error // webapi.DSM incorrect, this should be targetId
}

type DSMClient struct {
	webapi.DSM
}

func (dc *DSMClient) Init(
	host string,
	port int,
	user string,
	pass string,
	https bool,
) {
	dc.Ip = host
	dc.Port = port
	dc.Username = user
	dc.Password = pass
	dc.Https = https
}

var LUN_SPACE_RECLAMATION = webapi.LunDevAttrib{
	DevAttrib: "emulate_tpu",
	Enable:    1,
}

var LUN_FUA_WRITE = webapi.LunDevAttrib{
	DevAttrib: "emulate_fua_write",
	Enable:    1,
}

var LUN_SYNC_CACHE = webapi.LunDevAttrib{
	DevAttrib: "emulate_sync_cache",
	Enable:    1,
}

// Synology supports the following LUN types (and probably more):
// 3   - EXT4  thick, "FILE"
// 15  - EXT4  thin,  "ADV"
// 259 - BTRFS thick, "BLUN_THICK"
// 263 - BTRFS thin,  "BLUN"

func IsThin(lunType int) bool {
	switch lunType {
	case 3:
		return false
	case 15:
		return true
	case 259:
		return false
	case 263:
		return true
	default:
		return false
	}
}

func GetLunType(fsType string, thin bool) string {
	switch fsType {
	case "ext4":
		if thin {
			return "ADV"
		} else {
			return "FILE"
		}
	case "btrfs":
		if thin {
			return "BLUN"
		} else {
			return "BLUN_THICK"
		}
	default:
		return ""
	}
}
