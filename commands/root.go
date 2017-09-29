package commands

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"os/user"

	"golang.org/x/net/context"
	"golang.org/x/oauth2/google"

	"google.golang.org/api/compute/v1"
	"cloud.google.com/go/compute/metadata"

	"gopkg.in/inconshreveable/log15.v2"

	"github.com/docker/go-plugins-helpers/volume"
	"github.com/fsouza/go-dockerclient"
	"github.com/mcuadros/gce-docker/plugin"
	"github.com/mcuadros/gce-docker/watcher"
	"github.com/spf13/cobra"
)

type RootCommand struct {
	LogLevel string
	LogFile  string

	project  string
	zone     string
	instance string
	client   *http.Client
}

func NewRootCommand() *RootCommand {
	return &RootCommand{}
}

func (c *RootCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gce-docker",
		Short: "gce-docker - Google Cloud Engine integration for Docker",
		RunE:  c.Execute,
	}

	cmd.Flags().StringVar(&c.LogFile, "log-file", "", "log file")
	cmd.Flags().StringVar(&c.LogLevel, "log-level", "info", "max log level enabled")
	return cmd
}

func (c *RootCommand) Execute(cmd *cobra.Command, args []string) error {
	if err := c.checkGCE(); err != nil {
		return err
	}

	if err := c.loadMetadataInfo(); err != nil {
		return err
	}

	if err := c.setupLogging(); err != nil {
		return err
	}

	if err := c.buildComputeClient(); err != nil {
		return err
	}

	go func() {
		if err := c.runWatcher(); err != nil {
			log15.Crit(err.Error())
		}
	}()

	go func() {
		if err := c.runVolumePlugin(); err != nil {
			log15.Crit(err.Error())
		}
	}()

	select {}
	return nil
}

func (c *RootCommand) checkGCE() error {
	if !metadata.OnGCE() {
		return fmt.Errorf("gce-docker driver only runs on Google Compute Engine")
	}

	return nil
}

func (c *RootCommand) loadMetadataInfo() error {
	var err error
	c.instance, err = metadata.InstanceName()
	if err != nil {
		return fmt.Errorf("error retrieving instance name: %s", err)
	}

	c.zone, err = metadata.Zone()
	if err != nil {
		return fmt.Errorf("error retrieving zone: %s", err)
	}

	c.project, err = metadata.ProjectID()
	if err != nil {
		return fmt.Errorf("error retrieving project: %s", err)
	}

	return nil
}

func (c *RootCommand) setupLogging() error {
	lvl, err := log15.LvlFromString(c.LogLevel)
	if err != nil {
		return fmt.Errorf("unknown log level name %q", c.LogLevel)
	}

	handler := log15.StdoutHandler
	format := log15.LogfmtFormat()

	if c.LogFile != "" {
		handler = log15.MultiHandler(handler, log15.Must.FileHandler(c.LogFile, format))
	}

	handler = log15.LvlFilterHandler(lvl, handler)

	if lvl == log15.LvlDebug {
		handler = log15.CallerFileHandler(log15.LvlFilterHandler(lvl, handler))
	}

	log15.Root().SetHandler(handler)
	return nil
}

func (c *RootCommand) buildComputeClient() error {
	ctx := context.Background()

	var err error
	c.client, err = google.DefaultClient(ctx, compute.ComputeScope)
	if err != nil {
		return fmt.Errorf("error building compute client: %s", err)
	}

	return nil
}

func (c *RootCommand) runWatcher() error {
	log15.Info("starting watcher", "project", c.project, "zone", c.zone, "instance", c.instance)
	d, err := docker.NewClientFromEnv()
	if err != nil {
		return fmt.Errorf("error creating docker client: %s", err)
	}

	w, err := watcher.NewWatcher(d, c.client, c.project, c.zone, c.instance)
	if err != nil {
		return fmt.Errorf("error creating watcher: %s", err)
	}

	if err := w.Watch(); err != nil {
		return fmt.Errorf("error starting watcher: %s", err)
	}

	return nil
}

func (c *RootCommand) runVolumePlugin() error {
	log15.Info("starting volume driver", "project", c.project, "zone", c.zone, "instance", c.instance)
	d, err := plugin.NewVolume(c.client, c.project, c.zone, c.instance)
	if err != nil {
		return fmt.Errorf("error creating volume plugin: %s", err)
	}

	h := volume.NewHandler(d)
	u, _ := user.Lookup("root")
	gid, _ := strconv.Atoi(u.Gid)
	if err := h.ServeUnix("gce", gid); err != nil {
		return fmt.Errorf("error starting volume driver server: %s", err)
	}

	return nil
}

var RootCmd = NewRootCommand().Command()

func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(-1)
	}
}
