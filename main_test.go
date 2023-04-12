package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/SynologyOpenSource/synology-csi/pkg/dsm/webapi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pfrybar/syno-iscsi/syno"
)

var validCommand = []string{"", "--host", "host", "-port", "5000", "--user", "user", "--pass", "pass"}

var vol1 = webapi.VolInfo{
	Path:   "/vol1",
	Status: "normal",
	FsType: "ext4",
	Size:   fmt.Sprint(10 * gb),
	Free:   fmt.Sprint(5 * gb),
}

var vol2 = webapi.VolInfo{
	Path:   "/vol2",
	Status: "degraded",
	FsType: "btrfs",
	Size:   fmt.Sprint(5 * gb),
	Free:   fmt.Sprint(5 * gb),
}

var lun1 = webapi.LunInfo{
	Name:     "lun1",
	Uuid:     "c0416d61-e668-4fd9-86d7-7139c4fabd1d",
	LunType:  3, // EXT4, thick
	Location: "/vol1",
	Size:     5 * gb,
	Used:     3 * gb,
	Status:   "normal",
}

var lun2 = webapi.LunInfo{
	Name:     "lun2",
	Uuid:     "2391bb3d-82d9-4a64-b197-5faae2f8d95a",
	LunType:  263, // BTRFS, thin
	Location: "/vol2",
	Size:     5 * gb,
	Used:     0,
	Status:   "degraded",
}

var target1 = webapi.TargetInfo{
	Name:        "target1",
	Iqn:         "iqn.2000-01.com.synology:target1",
	MaxSessions: 2,
	MappedLuns: []webapi.MappedLun{
		{LunUuid: "c0416d61-e668-4fd9-86d7-7139c4fabd1d", MappingIndex: 0},
		{LunUuid: "2391bb3d-82d9-4a64-b197-5faae2f8d95a", MappingIndex: 1},
	},
	ConnectedSessions: []webapi.ConncetedSession{
		{Iqn: "iqn.1993-08.org.debian:client", Ip: "192.168.1.10"},
	},
	TargetId: 1,
}

var target2 = webapi.TargetInfo{
	Name:        "target2",
	Iqn:         "iqn.2000-01.com.synology:target2",
	MaxSessions: 1,
	MappedLuns: []webapi.MappedLun{
		{LunUuid: "c0416d61-e668-4fd9-86d7-7139c4fabd1d", MappingIndex: 0},
	},
	TargetId: 2,
}

func TestSynoIscsi(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SynoiSCSI Suite")
}

var _ = Describe("Main", func() {
	var buffer bytes.Buffer
	var reader bytes.Reader

	BeforeEach(func() {
		buffer = bytes.Buffer{}
		out = &buffer

		reader = bytes.Reader{}
		in = &reader

		synoClient = &MockSynoClient{}

		host = ""
		port = 0
		user = ""
		pass = ""
		https = false
	})

	Describe("readableByteSize works as expected", func() {
		toTest := map[uint64]string{
			0:                                       "0.00 B",
			1:                                       "1.00 B",
			1024:                                    "1.00 KiB",
			1024 * 1024:                             "1.00 MiB",
			1024 * 1024 * 1024:                      "1.00 GiB",
			1024 * 1024 * 1024 * 1024:               "1.00 TiB",
			1024 * 1024 * 1024 * 1024 * 1024:        "1.00 PiB",
			1024 * 1024 * 1024 * 1024 * 1024 * 1024: "1.00 EiB",

			5 * 1024: "5.00 KiB",
			123456:   "120.56 KiB",
		}

		for byteSize, expected := range toTest {
			Expect(readableByteSize(byteSize)).To(Equal(expected))
		}
	})

	Describe("Running any commands", func() {
		var (
			shost  string
			sport  int
			suser  string
			spass  string
			shttps bool

			login  bool
			logout bool
		)

		BeforeEach(func() {
			shost = ""
			sport = 0
			suser = ""
			spass = ""
			shttps = false
			login = false
			logout = false

			synoClient = &MockSynoClient{
				init: func(host string, port int, user string, pass string, https bool) {
					shost = host
					sport = port
					suser = user
					spass = pass
					shttps = https
				},
				login: func() error {
					login = true
					return nil
				},
				logout: func() error {
					logout = true
					return nil
				},
			}
		})

		It("will fail without global options", func() {
			cmd := []string{"", "volume", "list"}
			Expect(app.Run(cmd)).To(MatchError(fmt.Sprintf(missingGlobalArgsMsg, "host, user, pass")))

			cmd = []string{"", "--host", "host", "volume", "list"}
			Expect(app.Run(cmd)).To(MatchError(fmt.Sprintf(missingGlobalArgsMsg, "user, pass")))

			cmd = []string{"", "--host", "host", "--user", "user", "volume", "list"}
			Expect(app.Run(cmd)).To(MatchError(fmt.Sprintf(missingGlobalArgsMsg, "pass")))

			cmd = []string{"", "--host", "host", "--pass", "pass", "volume", "list"}
			Expect(app.Run(cmd)).To(MatchError(fmt.Sprintf(missingGlobalArgsMsg, "user")))
		})

		It("uses default global options", func() {
			cmd := []string{"", "--host", "host1", "--user", "user1", "--pass", "pass1", "volume", "list"}
			app.Run(cmd) // we don't care if it succeeds or fails

			Expect(sport).To(Equal(defaultPort))
			Expect(shttps).To(BeFalse())
		})

		It("uses environment variables for global options", func() {
			envs := map[string]string{
				hostEnvVar:  "host1",
				portEnvVar:  "1234",
				userEnvVar:  "user1",
				passEnvVar:  "pass1",
				httpsEnvVar: "true",
			}

			for env, value := range envs {
				os.Setenv(env, value)
			}

			cmd := []string{"", "volume", "list"}
			app.Run(cmd) // we don't care if it succeeds or fails

			Expect(shost).To(Equal("host1"))
			Expect(sport).To(Equal(1234))
			Expect(suser).To(Equal("user1"))
			Expect(spass).To(Equal("pass1"))
			Expect(shttps).To(BeTrue())

			for env := range envs {
				os.Unsetenv(env)
			}
		})

		DescribeTable("calls Init, Login, and Logout with expected parameters",
			func(command ...string) {
				cmd := []string{"", "--host", "host1", "--port", "1234", "--user", "user1", "--pass", "pass1", "--https"}
				cmd = append(cmd, command...)
				app.Run(cmd) // we don't care if it succeeds or fails

				Expect(shost).To(Equal("host1"))
				Expect(sport).To(Equal(1234))
				Expect(suser).To(Equal("user1"))
				Expect(spass).To(Equal("pass1"))
				Expect(shttps).To(BeTrue())
				Expect(login).To(BeTrue())
				Expect(logout).To(BeTrue())
			},
			// remember to add new commands here
			Entry("runs 'volume list'", "volume", "list"),
			Entry("runs 'lun list'", "lun", "list"),
			Entry("runs 'lun create ...'", "lun", "create", "lun1", "/vol1", "5"),
			Entry("runs 'lun delete ...'", "lun", "delete", "lun1"),
			Entry("runs 'lun map ...'", "lun", "map", "lun1", "target2"),
			Entry("runs 'target list'", "target", "list"),
			Entry("runs 'target create ...'", "target", "create", "target1", "iqn.2000-01.com.synology:target1"),
			Entry("runs 'target delete ...'", "target", "delete", "target1"),
		)
	})

	Describe("Listing volumes", func() {
		It("returns an error with the wrong number of arguments", func() {
			cmd := append(validCommand, "volume", "list", "none")
			Expect(app.Run(cmd)).To(MatchError(fmt.Sprintf(notEnoughArgsMsg, 0, 1)))
		})

		It("returns the expected result", func() {
			synoClient = &MockSynoClient{
				volumeList: func() ([]webapi.VolInfo, error) {
					return []webapi.VolInfo{vol1, vol2}, nil
				},
			}

			cmd := append(validCommand, "volume", "list")
			Expect(app.Run(cmd)).To(Succeed())

			output := buffer.String()
			lines := strings.Split(output, "\n")

			Expect(lines).To(HaveLen(4))

			line2 := lines[1]
			line2Terms := []string{"vol1", "normal", "ext4", "10.00 GiB", "5.00 GiB"}
			for _, term := range line2Terms {
				Expect(line2).To(ContainSubstring(term))
			}

			line3 := lines[2]
			line3Terms := []string{"vol2", "degraded", "btrfs", "5.00 GiB", "0 B"}
			for _, term := range line3Terms {
				Expect(line3).To(ContainSubstring(term))
			}
		})
	})

	Describe("Listing LUNs", func() {
		It("returns an error with the wrong number of arguments", func() {
			cmd := append(validCommand, "lun", "list", "none")
			Expect(app.Run(cmd)).To(MatchError(fmt.Sprintf(notEnoughArgsMsg, 0, 1)))
		})

		It("returns the expected result", func() {
			synoClient = &MockSynoClient{
				lunList: func() ([]webapi.LunInfo, error) {
					return []webapi.LunInfo{lun1, lun2}, nil
				},
			}

			cmd := append(validCommand, "lun", "list")
			Expect(app.Run(cmd)).To(Succeed())

			output := buffer.String()
			lines := strings.Split(output, "\n")

			Expect(lines).To(HaveLen(4))

			line2 := lines[1]
			line2Terms := []string{"lun1", "/vol1", "normal", "5.00 GiB", "3.00 GiB", "no"}
			for _, term := range line2Terms {
				Expect(line2).To(ContainSubstring(term))
			}

			line3 := lines[2]
			line3Terms := []string{"lun2", "/vol2", "degraded", "5.00 GiB", "0 B", "yes"}
			for _, term := range line3Terms {
				Expect(line3).To(ContainSubstring(term))
			}
		})
	})

	Describe("Creating LUNs", func() {
		It("returns an error with the wrong number of arguments", func() {
			cmd := append(validCommand, "lun", "create", "lun1", "/vol1")
			Expect(app.Run(cmd)).To(MatchError(fmt.Sprintf(notEnoughArgsMsg, 3, 2)))

			cmd = append(validCommand, "lun", "create", "lun1", "/vol1", "5", "none")
			Expect(app.Run(cmd)).To(MatchError(fmt.Sprintf(notEnoughArgsMsg, 3, 4)))
		})

		It("returns an error when using just the --reclaim flag", func() {
			cmd := append(validCommand, "lun", "create", "-r", "lun1", "/vol1", "5")
			Expect(app.Run(cmd)).To(MatchError(lunReclaimThinMsg))
		})

		It("returns an error with an invalid name", func() {
			cmd := append(validCommand, "lun", "create", "lun_1", "/vol1", "5")
			Expect(app.Run(cmd)).To(MatchError(lunInvalidNameMsg))

			cmd = append(validCommand, "lun", "create", " ", "/vol1", "5")
			Expect(app.Run(cmd)).To(MatchError(lunInvalidNameMsg))
		})

		It("returns an error with an invalid size", func() {
			cmd := append(validCommand, "lun", "create", "lun1", "/vol1", "abc")
			Expect(app.Run(cmd)).To(MatchError(lunInvalidSizeMsg))

			cmd = append(validCommand, "lun", "create", "lun1", "/vol1", "2.5")
			Expect(app.Run(cmd)).To(MatchError(lunInvalidSizeMsg))

			cmd = append(validCommand, "lun", "create", "lun1", "/vol1", "-5")
			Expect(app.Run(cmd)).To(MatchError(lunInvalidSizeMsg))
		})

		Context("with correct arguments", func() {
			var createSpec webapi.LunCreateSpec

			BeforeEach(func() {
				createSpec = webapi.LunCreateSpec{}
				synoClient = &MockSynoClient{
					volumeList: func() ([]webapi.VolInfo, error) {
						return []webapi.VolInfo{vol1, vol2}, nil
					},
					lunCreate: func(spec webapi.LunCreateSpec) (string, error) {
						createSpec = spec
						return "uuid", nil
					},
				}
			})

			It("returns an error for missing volume", func() {
				cmd := append(validCommand, "lun", "create", "lun1", "/vol3", "1")
				Expect(app.Run(cmd)).To(MatchError(fmt.Sprintf(volumeNotFoundMsg, "/vol3")))
			})

			It("returns an error if volume does not have enough space", func() {
				cmd := append(validCommand, "lun", "create", "lun1", "/vol1", "10")
				Expect(app.Run(cmd)).To(MatchError(fmt.Sprintf(volumeNotEnoughSpaceMsg, "/vol1", 5)))
			})

			It("creates a EXT4/thick LUN", func() {
				cmd := append(validCommand, "lun", "create", "lun1", "/vol1", "1")
				expectedSpec := webapi.LunCreateSpec{
					Name:       "lun1",
					Location:   "/vol1",
					Size:       1 * gb,
					Type:       "FILE",
					DevAttribs: []webapi.LunDevAttrib{},
				}
				Expect(app.Run(cmd)).To(Succeed())
				Expect(createSpec).To(Equal(expectedSpec))
				Expect(buffer.String()).To(Equal(lunCreatedMsg + "\n"))
			})

			It("creates a EXT4/thin LUN", func() {
				cmd := append(validCommand, "lun", "create", "--thin", "lun1", "/vol1", "1")
				expectedSpec := webapi.LunCreateSpec{
					Name:       "lun1",
					Location:   "/vol1",
					Size:       1 * gb,
					Type:       "ADV",
					DevAttribs: []webapi.LunDevAttrib{},
				}
				Expect(app.Run(cmd)).To(Succeed())
				Expect(createSpec).To(Equal(expectedSpec))
				Expect(buffer.String()).To(Equal(lunCreatedMsg + "\n"))
			})

			It("creates a BTRFS/thick LUN with --sync-cache", func() {
				cmd := append(validCommand, "lun", "create", "--sync-cache", "lun1", "/vol2", "1")
				expectedSpec := webapi.LunCreateSpec{
					Name:     "lun1",
					Location: "/vol2",
					Size:     1 * gb,
					Type:     "BLUN_THICK",
					DevAttribs: []webapi.LunDevAttrib{
						syno.LUN_FUA_WRITE, syno.LUN_SYNC_CACHE,
					},
				}
				Expect(app.Run(cmd)).To(Succeed())
				Expect(createSpec).To(Equal(expectedSpec))
				Expect(buffer.String()).To(Equal(lunCreatedMsg + "\n"))
			})

			It("creates a BTRFS/thin LUN with --reclaim", func() {
				cmd := append(validCommand, "lun", "create", "--thin", "--reclaim", "lun1", "/vol2", "1")
				expectedSpec := webapi.LunCreateSpec{
					Name:     "lun1",
					Location: "/vol2",
					Size:     1 * gb,
					Type:     "BLUN",
					DevAttribs: []webapi.LunDevAttrib{
						syno.LUN_SPACE_RECLAMATION,
					},
				}
				Expect(app.Run(cmd)).To(Succeed())
				Expect(createSpec).To(Equal(expectedSpec))
				Expect(buffer.String()).To(Equal(lunCreatedMsg + "\n"))
			})

			It("creates a BTRFS/thin LUN with -r (reclaim) and -s (sync-cache)", func() {
				cmd := append(validCommand, "lun", "create", "-trs", "lun1", "/vol2", "1")
				expectedSpec := webapi.LunCreateSpec{
					Name:     "lun1",
					Location: "/vol2",
					Size:     1 * gb,
					Type:     "BLUN",
					DevAttribs: []webapi.LunDevAttrib{
						syno.LUN_SPACE_RECLAMATION, syno.LUN_FUA_WRITE, syno.LUN_SYNC_CACHE,
					},
				}
				Expect(app.Run(cmd)).To(Succeed())
				Expect(createSpec).To(Equal(expectedSpec))
				Expect(buffer.String()).To(Equal(lunCreatedMsg + "\n"))
			})
		})
	})

	Describe("Deleting LUNs", func() {
		It("returns an error with the wrong number of arguments", func() {
			cmd := append(validCommand, "lun", "delete")
			Expect(app.Run(cmd)).To(MatchError(fmt.Sprintf(notEnoughArgsMsg, 1, 0)))

			cmd = append(validCommand, "lun", "delete", "lun1", "none")
			Expect(app.Run(cmd)).To(MatchError(fmt.Sprintf(notEnoughArgsMsg, 1, 2)))
		})

		Context("with correct arguments", func() {
			var uuid string

			BeforeEach(func() {
				uuid = ""
				synoClient = &MockSynoClient{
					lunList: func() ([]webapi.LunInfo, error) {
						return []webapi.LunInfo{lun1, lun2}, nil
					},
					lunDelete: func(lunUuid string) error {
						uuid = lunUuid
						return nil
					},
					targetList: func() ([]webapi.TargetInfo, error) {
						return []webapi.TargetInfo{target1, target2}, nil
					},
				}
			})

			It("returns an error for missing LUN", func() {
				cmd := append(validCommand, "lun", "delete", "lun3")
				Expect(app.Run(cmd)).To(MatchError(fmt.Sprintf(lunNotFoundMsg, "lun3")))
			})

			It("skips deletion if verification fails", func() {
				reader = *bytes.NewReader([]byte("nope"))
				cmd := append(validCommand, "lun", "delete", "lun1")
				Expect(app.Run(cmd)).To(Succeed())
				Expect(uuid).To(BeEmpty())
				output := buffer.String()
				Expect(output).To(ContainSubstring("sure you want to delete"))
				Expect(output).To(ContainSubstring("the lun name (lun1)"))
				Expect(output).To(ContainSubstring("Cancelled"))
			})

			It("deletes with verification by default", func() {
				reader = *bytes.NewReader([]byte("lun1"))
				cmd := append(validCommand, "lun", "delete", "lun1")
				Expect(app.Run(cmd)).To(Succeed())
				Expect(uuid).To(Equal("c0416d61-e668-4fd9-86d7-7139c4fabd1d"))
				output := buffer.String()
				Expect(output).To(ContainSubstring("sure you want to delete"))
				Expect(output).To(ContainSubstring("mapped to the targets"))
				Expect(output).To(ContainSubstring("target1 (connected), target2"))
				Expect(output).To(ContainSubstring("the lun name (lun1)"))
				Expect(output).To(ContainSubstring(lunDeletedMsg))
			})

			It("deletes without verification for --skip-verify", func() {
				cmd := append(validCommand, "lun", "delete", "--skip-verify", "lun1")
				Expect(app.Run(cmd)).To(Succeed())
				Expect(uuid).To(Equal("c0416d61-e668-4fd9-86d7-7139c4fabd1d"))
				Expect(buffer.String()).To(Equal(lunDeletedMsg + "\n"))
			})

			It("deletes without verification for -s (skip-verify)", func() {
				cmd := append(validCommand, "lun", "delete", "-s", "lun1")
				Expect(app.Run(cmd)).To(Succeed())
				Expect(uuid).To(Equal("c0416d61-e668-4fd9-86d7-7139c4fabd1d"))
				Expect(buffer.String()).To(Equal(lunDeletedMsg + "\n"))
			})
		})
	})

	Describe("Mapping LUNs", func() {
		It("returns an error with the wrong number of arguments", func() {
			cmd := append(validCommand, "lun", "map", "lun1")
			Expect(app.Run(cmd)).To(MatchError(fmt.Sprintf(notEnoughArgsMsg, 2, 1)))

			cmd = append(validCommand, "lun", "map", "lun1", "target1", "nope")
			Expect(app.Run(cmd)).To(MatchError(fmt.Sprintf(notEnoughArgsMsg, 2, 3)))
		})

		Context("with correct arguments", func() {
			var foundTargetUuids []string
			var foundLunUuid string

			BeforeEach(func() {
				foundTargetUuids = []string{}
				foundLunUuid = ""
				synoClient = &MockSynoClient{
					lunList: func() ([]webapi.LunInfo, error) {
						return []webapi.LunInfo{lun1, lun2}, nil
					},
					lunMapTarget: func(targetIds []string, lunUuid string) error {
						foundTargetUuids = targetIds
						foundLunUuid = lunUuid
						return nil
					},
					targetList: func() ([]webapi.TargetInfo, error) {
						return []webapi.TargetInfo{target1, target2}, nil
					},
				}
			})

			It("returns an error for missing LUN", func() {
				cmd := append(validCommand, "lun", "map", "lun3", "target1")
				Expect(app.Run(cmd)).To(MatchError(fmt.Sprintf(lunNotFoundMsg, "lun3")))
			})

			It("returns an error for missing target", func() {
				cmd := append(validCommand, "lun", "map", "lun1", "target3")
				Expect(app.Run(cmd)).To(MatchError(fmt.Sprintf(targetNotFoundMsg, "target3")))
			})

			It("maps LUN to the target", func() {
				cmd := append(validCommand, "lun", "map", "lun1", "target1")
				Expect(app.Run(cmd)).To(Succeed())
				Expect(foundTargetUuids).To(Equal([]string{"1"}))
				Expect(foundLunUuid).To(Equal(lun1.Uuid))
				Expect(buffer.String()).To(Equal(lunMappedMsg + "\n"))
			})
		})
	})

	Describe("Listing targets", func() {
		It("returns an error with the wrong number of arguments", func() {
			cmd := append(validCommand, "target", "list", "none")
			Expect(app.Run(cmd)).To(MatchError(fmt.Sprintf(notEnoughArgsMsg, 0, 1)))
		})

		It("returns the expected result", func() {
			synoClient = &MockSynoClient{
				lunList: func() ([]webapi.LunInfo, error) {
					return []webapi.LunInfo{lun1, lun2}, nil
				},
				targetList: func() ([]webapi.TargetInfo, error) {
					return []webapi.TargetInfo{target1, target2}, nil
				},
			}

			cmd := append(validCommand, "target", "list")
			Expect(app.Run(cmd)).To(Succeed())

			output := buffer.String()
			lines := strings.Split(output, "\n")

			Expect(lines).To(HaveLen(4))

			line2 := lines[1]
			line2Terms := []string{"target1", "iqn.2000-01.com.synology:target1", "1/2", "lun1,lun2"}
			for _, term := range line2Terms {
				Expect(line2).To(ContainSubstring(term))
			}

			line3 := lines[2]
			line3Terms := []string{"target2", "iqn.2000-01.com.synology:target2", "0/1", "lun1"}
			for _, term := range line3Terms {
				Expect(line3).To(ContainSubstring(term))
			}
		})
	})

	Describe("Creating targets", func() {
		It("returns an error with the wrong number of arguments", func() {
			cmd := append(validCommand, "target", "create", "target1")
			Expect(app.Run(cmd)).To(MatchError(fmt.Sprintf(notEnoughArgsMsg, 2, 1)))

			cmd = append(validCommand, "target", "create", "target1", "iqn.2000-01.com.synology:target1", "none")
			Expect(app.Run(cmd)).To(MatchError(fmt.Sprintf(notEnoughArgsMsg, 2, 3)))
		})

		Context("with correct arguments", func() {
			var createSpec webapi.TargetCreateSpec

			BeforeEach(func() {
				createSpec = webapi.TargetCreateSpec{}
				synoClient = &MockSynoClient{
					targetCreate: func(spec webapi.TargetCreateSpec) (string, error) {
						createSpec = spec
						return "uuid", nil
					},
				}
			})

			It("creates a target", func() {
				cmd := append(validCommand, "target", "create", "target1", "iqn.2000-01.com.synology:target1")
				expectedSpec := webapi.TargetCreateSpec{
					Name: "target1",
					Iqn:  "iqn.2000-01.com.synology:target1",
				}
				Expect(app.Run(cmd)).To(Succeed())
				Expect(createSpec).To(Equal(expectedSpec))
				Expect(buffer.String()).To(Equal(targetCreatedMsg + "\n"))
			})
		})
	})

	Describe("Deleting targets", func() {
		It("returns an error with the wrong number of arguments", func() {
			cmd := append(validCommand, "target", "delete")
			Expect(app.Run(cmd)).To(MatchError(fmt.Sprintf(notEnoughArgsMsg, 1, 0)))

			cmd = append(validCommand, "target", "delete", "target1", "none")
			Expect(app.Run(cmd)).To(MatchError(fmt.Sprintf(notEnoughArgsMsg, 1, 2)))
		})

		Context("with correct arguments", func() {
			var id string

			BeforeEach(func() {
				id = ""
				synoClient = &MockSynoClient{
					targetList: func() ([]webapi.TargetInfo, error) {
						return []webapi.TargetInfo{target1, target2}, nil
					},
					targetDelete: func(targetName string) error {
						id = targetName
						return nil
					},
				}
			})

			It("returns an error for missing target", func() {
				cmd := append(validCommand, "target", "delete", "target3")
				Expect(app.Run(cmd)).To(MatchError(fmt.Sprintf(targetNotFoundMsg, "target3")))
			})

			It("skips deletion if verification fails", func() {
				reader = *bytes.NewReader([]byte("nope"))
				cmd := append(validCommand, "target", "delete", "target2")
				Expect(app.Run(cmd)).To(Succeed())
				Expect(id).To(BeEmpty())
				output := buffer.String()
				Expect(output).To(ContainSubstring("sure you want to delete"))
				Expect(output).To(ContainSubstring("the target name (target2)"))
				Expect(output).To(ContainSubstring("Cancelled"))
			})

			It("deletes with verification by default", func() {
				reader = *bytes.NewReader([]byte("target2"))
				cmd := append(validCommand, "target", "delete", "target2")
				Expect(app.Run(cmd)).To(Succeed())
				Expect(id).To(Equal("2"))
				output := buffer.String()
				Expect(output).To(ContainSubstring("sure you want to delete"))
				Expect(output).To(ContainSubstring("the target name (target2)"))
				Expect(output).To(ContainSubstring(targetDeletedMsg))
			})

			It("deletes without verification for --skip-verify", func() {
				cmd := append(validCommand, "target", "delete", "--skip-verify", "target2")
				Expect(app.Run(cmd)).To(Succeed())
				Expect(id).To(Equal("2"))
				Expect(buffer.String()).To(Equal(targetDeletedMsg + "\n"))
			})

			It("skips deletion if active sessions exist", func() {
				cmd := append(validCommand, "target", "delete", "target1")
				Expect(app.Run(cmd)).To(Succeed())
				Expect(id).To(BeEmpty())
				output := buffer.String()
				Expect(output).To(Equal(targetActiveSessionMsg + "\n"))
			})

			It("allows force deletion with active sessions", func() {
				reader = *bytes.NewReader([]byte("target1"))
				cmd := append(validCommand, "target", "delete", "--force", "target1")
				Expect(app.Run(cmd)).To(Succeed())
				Expect(id).To(Equal("1"))
				output := buffer.String()
				Expect(output).To(ContainSubstring(targetForceDeleteMsg))
				Expect(output).To(ContainSubstring("sure you want to delete"))
				Expect(output).To(ContainSubstring("the target name (target1)"))
				Expect(output).To(ContainSubstring(targetDeletedMsg))
			})

			It("force deletes without verification for -fs (force and skip-verify) ", func() {
				cmd := append(validCommand, "target", "delete", "-fs", "target1")
				Expect(app.Run(cmd)).To(Succeed())
				Expect(id).To(Equal("1"))
				output := buffer.String()
				Expect(output).To(ContainSubstring(targetForceDeleteMsg))
				Expect(output).To(ContainSubstring(targetDeletedMsg))
			})
		})
	})
})

type MockSynoClient struct {
	init         func(host string, port int, user string, pass string, https bool)
	login        func() error
	logout       func() error
	volumeList   func() ([]webapi.VolInfo, error)
	lunList      func() ([]webapi.LunInfo, error)
	lunCreate    func(spec webapi.LunCreateSpec) (string, error)
	lunDelete    func(lunUuid string) error
	lunMapTarget func(targetIds []string, lunUuid string) error
	targetList   func() ([]webapi.TargetInfo, error)
	targetCreate func(spec webapi.TargetCreateSpec) (string, error)
	targetDelete func(targetName string) error
}

func (m *MockSynoClient) Init(host string, port int, user string, pass string, https bool) {
	if m.init != nil {
		m.init(host, port, user, pass, https)
	}
}

func (m *MockSynoClient) Login() error {
	if m.login != nil {
		return m.login()
	}
	return nil
}

func (m *MockSynoClient) Logout() error {
	if m.logout != nil {
		return m.logout()
	}
	return nil
}

func (m *MockSynoClient) VolumeList() ([]webapi.VolInfo, error) {
	if m.volumeList != nil {
		return m.volumeList()
	}
	return []webapi.VolInfo{}, nil
}

func (m *MockSynoClient) LunList() ([]webapi.LunInfo, error) {
	if m.lunList != nil {
		return m.lunList()
	}
	return []webapi.LunInfo{}, nil
}

func (m *MockSynoClient) LunCreate(spec webapi.LunCreateSpec) (string, error) {
	if m.lunCreate != nil {
		return m.lunCreate(spec)
	}
	return "", nil
}

func (m *MockSynoClient) LunDelete(lunUuid string) error {
	if m.lunDelete != nil {
		return m.lunDelete((lunUuid))
	}
	return nil
}

func (m *MockSynoClient) LunMapTarget(targetIds []string, lunUuid string) error {
	if m.lunMapTarget != nil {
		return m.lunMapTarget(targetIds, lunUuid)
	}
	return nil
}

func (m *MockSynoClient) TargetList() ([]webapi.TargetInfo, error) {
	if m.targetList != nil {
		return m.targetList()
	}
	return []webapi.TargetInfo{}, nil
}

func (m *MockSynoClient) TargetCreate(spec webapi.TargetCreateSpec) (string, error) {
	if m.targetCreate != nil {
		return m.targetCreate(spec)
	}
	return "", nil
}

func (m *MockSynoClient) TargetDelete(targetName string) error {
	if m.targetDelete != nil {
		return m.targetDelete(targetName)
	}
	return nil
}
