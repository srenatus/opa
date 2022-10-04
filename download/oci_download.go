package download

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/oci"
	oras_reg "oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"

	"github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/metrics"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/plugins/rest"
	"github.com/open-policy-agent/opa/util"
)

type OCIDownloader struct {
	config         Config                        // downloader configuration for tuning polling and other downloader behaviour
	client         rest.Client                   // HTTP client to use for bundle downloading
	path           string                        // path for OCI image as <registry>/<org>/<repo>:<tag>
	localStorePath string                        // path for the local OCI storage
	trigger        chan chan struct{}            // channel to signal downloads when manual triggering is enabled
	stop           chan chan struct{}            // used to signal plugin to stop running
	f              func(context.Context, Update) // callback function invoked when download updates occur
	sizeLimitBytes *int64                        // max bundle file size in bytes (passed to reader)
	bvc            *bundle.VerificationConfig
	wg             sync.WaitGroup
	logger         logging.Logger
	mtx            sync.Mutex
	stopped        bool
	persist        bool
	store          *oci.Store
}

// New returns a new Downloader that can be started.
func NewOCI(config Config, client rest.Client, path, storePath string) *OCIDownloader {
	store, err := oci.New(storePath)
	if err != nil {
		panic(err)
	}
	return &OCIDownloader{
		config:         config,
		path:           path,
		localStorePath: storePath,
		client:         client,
		trigger:        make(chan chan struct{}),
		stop:           make(chan chan struct{}),
		logger:         client.Logger(),
		store:          store,
	}
}

// WithCallback registers a function f to be called when download updates occur.
func (d *OCIDownloader) WithCallback(f func(context.Context, Update)) *OCIDownloader {
	d.f = f
	return d
}

// WithLogAttrs sets an optional set of key/value pair attributes to include in
// log messages emitted by the downloader.
func (d *OCIDownloader) WithLogAttrs(attrs map[string]interface{}) *OCIDownloader {
	d.logger = d.logger.WithFields(attrs)
	return d
}

// WithBundleVerificationConfig sets the key configuration used to verify a signed bundle
func (d *OCIDownloader) WithBundleVerificationConfig(config *bundle.VerificationConfig) *OCIDownloader {
	d.bvc = config
	return d
}

// WithSizeLimitBytes sets the file size limit for bundles read by this downloader.
func (d *OCIDownloader) WithSizeLimitBytes(n int64) *OCIDownloader {
	d.sizeLimitBytes = &n
	return d
}

// WithBundlePersistence specifies if the downloaded bundle will eventually be persisted to disk.
func (d *OCIDownloader) WithBundlePersistence(persist bool) *OCIDownloader {
	d.persist = persist
	return d
}

// TODO: remove method ClearCache is deprecated. Use SetCache instead.
func (d *OCIDownloader) ClearCache() {
}

// TODO: this will need implementation for the OCI downloader.
func (d *OCIDownloader) SetCache(etag string) {
}

// Trigger can be used to control when the downloader attempts to download
// a new bundle in manual triggering mode.
func (d *OCIDownloader) Trigger(ctx context.Context) error {
	done := make(chan error)

	go func() {
		err := d.oneShot(ctx)
		if err != nil {
			d.logger.Error("OCI - Bundle download failed: %v.", err)
			if ctx.Err() == nil {
				done <- err
			}
		}
		close(done)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Start tells the Downloader to begin downloading bundles.
func (d *OCIDownloader) Start(ctx context.Context) {
	if *d.config.Trigger == plugins.TriggerPeriodic {
		go d.doStart(ctx)
	}
}

// Stop tells the Downloader to stop downloading bundles.
func (d *OCIDownloader) Stop(context.Context) {
	if *d.config.Trigger == plugins.TriggerManual {
		return
	}

	d.mtx.Lock()
	defer d.mtx.Unlock()

	if d.stopped {
		return
	}

	done := make(chan struct{})
	d.stop <- done
	<-done
}

func (d *OCIDownloader) doStart(context.Context) {
	// We'll revisit context passing/usage later.
	ctx, cancel := context.WithCancel(context.Background())

	d.wg.Add(1)
	go d.loop(ctx)

	done := <-d.stop // blocks until there's something to read
	cancel()
	d.wg.Wait()
	d.stopped = true
	close(done)
}

func (d *OCIDownloader) loop(ctx context.Context) {
	defer d.wg.Done()

	var retry int

	for {

		var delay time.Duration

		err := d.oneShot(ctx)

		if ctx.Err() != nil {
			return
		}

		if err != nil {
			delay = util.DefaultBackoff(float64(minRetryDelay), float64(*d.config.Polling.MaxDelaySeconds), retry)
		} else {
			// revert the response header timeout value on the http client's transport
			min := float64(*d.config.Polling.MinDelaySeconds)
			max := float64(*d.config.Polling.MaxDelaySeconds)
			delay = time.Duration(((max - min) * rand.Float64()) + min)
		}

		d.logger.Debug("OCI - Waiting %v before next download/retry.", delay)

		select {
		case <-time.After(delay):
			if err != nil {
				retry++
			} else {
				retry = 0
			}
		case <-ctx.Done():
			return
		}
	}
}
func (d *OCIDownloader) oneShot(ctx context.Context) error {
	m := metrics.New()
	resp, err := d.download(ctx, m)
	if err != nil {
		if d.f != nil {
			d.f(ctx, Update{ETag: "", Bundle: nil, Error: err, Metrics: m, Raw: nil})
		}
		return err
	}

	if d.f != nil {
		d.f(ctx, Update{ETag: resp.etag, Bundle: resp.b, Error: nil, Metrics: m, Raw: resp.raw})
	}
	return nil
}

func (d *OCIDownloader) download(ctx context.Context, m metrics.Metrics) (*downloaderResponse, error) {
	d.logger.Debug("OCI - Download starting.")

	d.client = d.client.WithHeader("Prefer", fmt.Sprintf("modes=%v,%v", defaultBundleMode, deltaBundleMode))

	m.Timer(metrics.BundleRequest).Start()
	md, err := d.pull(ctx, d.path)
	if err != nil {
		return &downloaderResponse{}, fmt.Errorf("failed to pull %s: %w", d.path, err)
	}

	if md.MediaType == "application/vnd.oci.image.layer.v1.tar+gzip" {
		return nil, fmt.Errorf("no tarball descriptor found in the layers")
	}

	bundleFilePath := filepath.Join(d.localStorePath, "blobs", "sha256", string(md.Digest.Hex()))
	fileReader, err := os.Open(bundleFilePath)
	if err != nil {
		return nil, err
	}
	loader := bundle.NewTarballLoaderWithBaseURL(fileReader, d.localStorePath)
	reader := bundle.NewCustomReader(loader).WithBaseDir(d.localStorePath)
	bundleInfo, err := reader.Read()
	if err != nil {
		return &downloaderResponse{}, fmt.Errorf("unexpected error %w", err)
	}

	m.Timer(metrics.BundleRequest).Stop()

	return &downloaderResponse{
		b:        &bundleInfo,
		raw:      fileReader,
		etag:     "",
		longPoll: false,
	}, nil
}

func (d *OCIDownloader) pull(ctx context.Context, ref string) (*ocispec.Descriptor, error) {
	client, err := d.getHTTPClient()
	if err != nil {
		return nil, err
	}
	urlInfo, err := url.Parse(d.client.Config().URL)
	if err != nil {
		return nil, fmt.Errorf("invalid host url %s: %w", d.client.Config().URL, err)
	}

	registry := urlInfo.Host
	reg, err := remote.NewRegistry(registry)
	if err != nil {
		return nil, fmt.Errorf("1download for '%s' failed (registry %q): %w", ref, registry, err)
	}
	reg.Client = client

	rf, err := oras_reg.ParseReference(ref)
	if err != nil {
		return nil, err
	}

	src, err := reg.Repository(ctx, rf.Repository)
	if err != nil {
		return nil, fmt.Errorf("2download for '%s' failed: %w", ref, err)
	}
	manifestDescriptor, err := oras.Copy(ctx, src, ref, d.store, "", oras.DefaultCopyOptions)
	if err != nil {
		return nil, fmt.Errorf("3download for '%s' failed: %w", ref, err)
	}

	return &manifestDescriptor, nil
}

func (d *OCIDownloader) getHTTPClient() (*auth.Client, error) {
	clientConfig := d.client.Config()
	client := auth.DefaultClient
	var err error

	switch {
	case clientConfig == nil: // default client, at the end of this function

	case clientConfig.Credentials.ClientTLS != nil:
		client.Client, err = clientConfig.Credentials.ClientTLS.NewClient(*clientConfig)
		if err != nil {
			return nil, fmt.Errorf("can not create a new client: %w", err)
		}
		return client, nil

	case clientConfig.Credentials.Bearer != nil:
		client.Client, err = clientConfig.Credentials.Bearer.NewClient(*clientConfig)
		if err != nil {
			return nil, fmt.Errorf("can not create a new bearer client: %w", err)
		}

		client.Header.Add("Authorization", fmt.Sprintf("%s %s",
			clientConfig.Credentials.Bearer.Scheme,
			base64.StdEncoding.EncodeToString([]byte(clientConfig.Credentials.Bearer.Token))),
		)
		return client, nil
	}

	client.Client = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: clientConfig.AllowInsecureTLS}}}
	return client, nil
}
