package plugin

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/mcuadros/gce-docker/providers"

	"github.com/docker/go-plugins-helpers/volume"
	"gopkg.in/inconshreveable/log15.v2"
)

var WaitStatusTimeout = 100 * time.Second

type Volume struct {
	Root string
	p    providers.DiskProvider
	fs   Filesystem
}

func NewVolume(c *http.Client, project, zone, instance string) (*Volume, error) {
	p, err := providers.NewDisk(c, project, zone, instance)
	if err != nil {
		return nil, err
	}

	return &Volume{
		Root: "/mnt/",
		p:    p,
		fs:   NewFilesystem(),
	}, nil
}

func (v *Volume) Create(r *volume.CreateRequest) error {
	log15.Debug("create request received", "name", r.Name)
	start := time.Now()
	config, err := v.createDiskConfig(r.Name, r)
	if err != nil {
		return err
	}

	if err := v.p.Create(config); err != nil {
		return err
	}

	log15.Info("disk created", "disk", r.Name, "elapsed", time.Since(start))
	return nil
}

func (v *Volume) List() (*volume.ListResponse, error) {
	log15.Debug("list request received")
	disks, err := v.p.List()
	if err != nil {
		return nil, err
	}

	r := volume.ListResponse{}
	for _, d := range disks {
		if d.Status != "READY" {
			continue
		}

		r.Volumes = append(r.Volumes, &volume.Volume{
			Name: d.Name,
		})
	}

	return &r, nil
}

func (v *Volume) Capabilities() *volume.CapabilitiesResponse {
	log15.Debug("capabilities request received")
	return &volume.CapabilitiesResponse{
		Capabilities: volume.Capability{Scope: "local"},
	}
}

func (v *Volume) Get(r *volume.GetRequest) (*volume.GetResponse, error) {
	log15.Debug("get request received")
	disks, err := v.p.List()
	if err != nil {
		return nil, err
	}

	resp := volume.GetResponse{}
	for _, d := range disks {
		if d.Name != r.Name {
			continue
		}

		config, err := v.createDiskConfig(r.Name, nil)
		if err != nil {
			return nil, err
		}

		resp.Volume = &volume.Volume{
			Name:       d.Name,
			Mountpoint: config.MountPoint(v.Root),
		}
	}

	return &resp, nil
}

func (v *Volume) Remove(r *volume.RemoveRequest) error {
	log15.Debug("remove request received", "name", r.Name)
	start := time.Now()

	config, err := v.createDiskConfig(r.Name, nil)
	if err != nil {
		return err
	}

	if err := v.p.Delete(config); err != nil {
		return err
	}

	log15.Info("disk removed", "disk", r.Name, "elapsed", time.Since(start))
	return nil
}

func (v *Volume) Path(r *volume.PathRequest) (*volume.PathResponse, error) {
	config, err := v.createDiskConfig(r.Name, nil)
	if err != nil {
		return nil, err
	}

	mnt := config.MountPoint(v.Root)
	log15.Debug("path request received", "name", r.Name, "mnt", mnt)

	if err := v.createMountPoint(config); err != nil {
		return nil, err
	}

	return &volume.PathResponse{Mountpoint: mnt}, nil
}

func (v *Volume) Mount(r *volume.MountRequest) (*volume.MountResponse, error) {
	log15.Debug("mount request received", "name", r.Name)
	start := time.Now()

	config, err := v.createDiskConfig(r.Name, nil)
	if err != nil {
		return nil, err
	}

	if err := v.createMountPoint(config); err != nil {
		return nil, err
	}

	if err := v.p.Attach(config); err != nil {
		return nil, err
	}

	if err := v.fs.Format(config.Dev()); err != nil {
		return nil, err
	}

	if err := v.fs.Mount(config.Dev(), config.MountPoint(v.Root)); err != nil {
		return nil, err
	}

	log15.Info("disk mounted", "disk", r.Name, "elapsed", time.Since(start))
	return &volume.MountResponse{
		Mountpoint: config.MountPoint(v.Root),
	}, nil
}

func (v *Volume) createMountPoint(c *providers.DiskConfig) error {
	target := c.MountPoint(v.Root)
	fi, err := v.fs.Stat(target)
	if os.IsNotExist(err) {
		return v.fs.MkdirAll(target, 0755)
	}

	if err != nil {
		return err
	}

	if !fi.IsDir() {
		return fmt.Errorf("error the mountpoint %q already exists", target)
	}

	return nil
}

func (v *Volume) Unmount(r *volume.UnmountRequest) error {
	log15.Debug("unmount request received", "name", r.Name)
	start := time.Now()
	config, err := v.createDiskConfig(r.Name, nil)
	if err != nil {
		return err
	}

	if err := v.fs.Unmount(config.MountPoint(v.Root)); err != nil {
		return err
	}

	if err := v.p.Detach(config); err != nil {
		return err
	}

	log15.Info("disk unmounted", "disk", r.Name, "elapsed", time.Since(start))
	return nil
}

func (v *Volume) createDiskConfig(name string, r *volume.CreateRequest) (*providers.DiskConfig, error) {
	config := &providers.DiskConfig{Name: name}

	if r != nil {
		for key, value := range r.Options {
			switch key {
			case "Name":
				config.Name = value
			case "Type":
				config.Type = value
			case "SizeGb":
				var err error
				config.SizeGb, err = strconv.ParseInt(value, 10, 64)
				if err != nil {
					return nil, err
				}
			case "SourceSnapshot":
				config.SourceSnapshot = value
			case "SourceImage":
				config.SourceImage = value
			default:
				return nil, fmt.Errorf("unknown option %q", key)
			}
		}
	}

	return config, config.Validate()
}
