package cluster

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/pinkerton/layer"
	"github.com/flynn/flynn/pkg/httpclient"
	"github.com/flynn/flynn/pkg/stream"
)

// Host is a client for a host daemon.
type Host interface {
	// ID returns the ID of the host this client communicates with.
	ID() string

	// ListJobs lists the jobs running on the host.
	ListJobs() (map[string]host.ActiveJob, error)

	// GetJob retrieves job details by ID.
	GetJob(id string) (*host.ActiveJob, error)

	// StopJob stops a running job.
	StopJob(id string) error

	// StreamEvents about job state changes to ch. id may be "all" or a single
	// job ID.
	StreamEvents(id string, ch chan<- *host.Event) (stream.Stream, error)

	// Attach attaches to a job, optionally waiting for it to start before
	// attaching.
	Attach(req *host.AttachReq, wait bool) (AttachClient, error)

	// Creates a new volume, returning its ID.
	// When in doubt, use a providerId of "default".
	CreateVolume(providerId string) (*volume.Info, error)

	DestroyVolume(volumeID string) error

	CreateSnapshot(volumeID string) (*volume.Info, error)

	// Requests the host pull a snapshot from another host onto one of its volumes.
	// Returns the info for the new snapshot.
	PullSnapshot(receiveVolID string, sourceHostID string, sourceSnapID string) (*volume.Info, error)

	// Request transfer of volume snapshot data
	// (this is used by other hosts in service of the PullSnapshot request).
	SendSnapshot(snapID string, assumeHaves []json.RawMessage) (io.ReadCloser, error)

	// PullImages pulls images from a TUF repository using the local TUF file in tufDB
	PullImages(repository, driver, root string, tufDB io.Reader, ch chan<- *layer.PullInfo) (stream.Stream, error)
}

type hostClient struct {
	id string
	c  *httpclient.Client
}

// NewHostClient creates a new Host that uses client to communicate with it.
// addr is used by Attach.
func NewHostClient(id string, addr string, h *http.Client) Host {
	if h == nil {
		h = http.DefaultClient
	}
	return &hostClient{
		id: id,
		c: &httpclient.Client{
			ErrNotFound: ErrNotFound,
			URL:         addr,
			HTTP:        h,
		},
	}
}

func (c *hostClient) ID() string {
	return c.id
}

func (c *hostClient) ListJobs() (map[string]host.ActiveJob, error) {
	var jobs map[string]host.ActiveJob
	err := c.c.Get("/host/jobs", &jobs)
	return jobs, err
}

func (c *hostClient) GetJob(id string) (*host.ActiveJob, error) {
	var res host.ActiveJob
	err := c.c.Get(fmt.Sprintf("/host/jobs/%s", id), &res)
	return &res, err
}

func (c *hostClient) StopJob(id string) error {
	return c.c.Delete(fmt.Sprintf("/host/jobs/%s", id))
}

func (c *hostClient) StreamEvents(id string, ch chan<- *host.Event) (stream.Stream, error) {
	r := fmt.Sprintf("/host/jobs/%s", id)
	if id == "all" {
		r = "/host/jobs"
	}
	return c.c.Stream("GET", r, nil, ch)
}

func (c *hostClient) CreateVolume(providerId string) (*volume.Info, error) {
	var res volume.Info
	err := c.c.Post(fmt.Sprintf("/storage/providers/%s/volumes", providerId), nil, &res)
	return &res, err
}

func (c *hostClient) DestroyVolume(volumeID string) error {
	return c.c.Post(fmt.Sprintf("/storage/volumes/%s/destroy", volumeID), nil, nil)
}

func (c *hostClient) CreateSnapshot(volumeID string) (*volume.Info, error) {
	var res volume.Info
	err := c.c.Put(fmt.Sprintf("/storage/volumes/%s/snapshot", volumeID), nil, &res)
	return &res, err
}

func (c *hostClient) PullSnapshot(receiveVolID string, sourceHostID string, sourceSnapID string) (*volume.Info, error) {
	var res volume.Info
	pull := volume.PullCoordinate{
		HostID:     sourceHostID,
		SnapshotID: sourceSnapID,
	}
	err := c.c.Post(fmt.Sprintf("/storage/volumes/%s/pull_snapshot", receiveVolID), pull, &res)
	return &res, err
}

func (c *hostClient) SendSnapshot(snapID string, assumeHaves []json.RawMessage) (io.ReadCloser, error) {
	header := http.Header{
		"Accept": []string{"application/vnd.zfs.snapshot-stream"},
	}
	res, err := c.c.RawReq("GET", fmt.Sprintf("/storage/volumes/%s/send", snapID), header, assumeHaves, nil)
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

func (c *hostClient) PullImages(repository, driver, root string, tufDB io.Reader, ch chan<- *layer.PullInfo) (stream.Stream, error) {
	header := http.Header{"Content-Type": {"application/octet-stream"}}
	path := fmt.Sprintf("/host/pull-images?repository=%s&driver=%s&root=%s", repository, driver, root)
	return c.c.StreamWithHeader("POST", path, header, tufDB, ch)
}
