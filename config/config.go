package config

import (
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"flag"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/cshum/imagor/metrics/prometheusmetrics"
	"github.com/cshum/imagor/storage/filestorage"

	"github.com/cshum/imagor"
	"github.com/cshum/imagor/imagorpath"
	"github.com/cshum/imagor/server"
	"github.com/peterbourgon/ff/v3"
	"go.uber.org/zap"
)

var baseConfig = []Option{
	withFileSystem,
	withHTTPLoader,
	withPrefilterStorage,
}

// NewImagor create imagor from config flags
func NewImagor(
	fs *flag.FlagSet, cb func() (*zap.Logger, bool), funcs ...Option,
) *imagor.Imagor {
	var (
		imagorSecret = fs.String("imagor-secret", "",
			"Secret key for signing imagor URL")
		imagorUnsafe = fs.Bool("imagor-unsafe", false,
			"Unsafe imagor that does not require URL signature. Prone to URL tampering")
		imagorAutoWebP = fs.Bool("imagor-auto-webp", false,
			"Output WebP format automatically if browser supports")
		imagorAutoAVIF = fs.Bool("imagor-auto-avif", false,
			"Output AVIF format automatically if browser supports (experimental)")
		imagorRequestTimeout = fs.Duration("imagor-request-timeout",
			time.Second*30, "Timeout for performing imagor request")
		imagorLoadTimeout = fs.Duration("imagor-load-timeout",
			0, "Timeout for imagor Loader request, should be smaller than imagor-request-timeout")
		imagorSaveTimeout = fs.Duration("imagor-save-timeout",
			0, "Timeout for saving image to imagor Storage")
		imagorProcessTimeout = fs.Duration("imagor-process-timeout",
			0, "Timeout for image processing")
		imagorBasePathRedirect = fs.String("imagor-base-path-redirect", "",
			"URL to redirect for imagor / base path e.g. https://www.google.com")
		imagorBaseParams = fs.String("imagor-base-params", "",
			"imagor endpoint base params that applies to all resulting images e.g. filters:watermark(example.jpg)")
		imagorProcessConcurrency = fs.Int64("imagor-process-concurrency",
			-1, "Maximum number of image process to be executed simultaneously. Requests that exceed this limit are put in the queue. Set -1 for no limit")
		imagorProcessQueueSize = fs.Int64("imagor-process-queue-size",
			0, "Maximum number of image process that can be put in the queue. Requests that exceed this limit are rejected with HTTP status 429")
		imagorCacheHeaderTTL = fs.Duration("imagor-cache-header-ttl",
			time.Hour*24*7, "imagor HTTP Cache-Control header TTL for successful image response")
		imagorCacheHeaderSWR = fs.Duration("imagor-cache-header-swr",
			time.Hour*24, "imagor HTTP Cache-Control header stale-while-revalidate for successful image response")
		imagorCacheHeaderNoCache = fs.Bool("imagor-cache-header-no-cache",
			false, "imagor HTTP Cache-Control header no-cache for successful image response")
		imagorModifiedTimeCheck = fs.Bool("imagor-modified-time-check", false,
			"Check modified time of result image against the source image. This eliminates stale result but require more lookups")
		imagorDisableErrorBody       = fs.Bool("imagor-disable-error-body", false, "imagor disable response body on error")
		imagorDisableParamsEndpoint  = fs.Bool("imagor-disable-params-endpoint", false, "imagor disable /params endpoint")
		imagorSignerType             = fs.String("imagor-signer-type", "sha1", "imagor URL signature hasher type: sha1, sha256, sha512")
		imagorSignerTruncate         = fs.Int("imagor-signer-truncate", 0, "imagor URL signature truncate at length")
		imagorStoragePathStyle       = fs.String("imagor-storage-path-style", "original", "imagor storage path style: original, digest")
		imagorResultStoragePathStyle = fs.String("imagor-result-storage-path-style", "original", "imagor result storage path style: original, digest, suffix")

		// Prefilter configuration options
		imagorPrefilterDepthmapAPI = fs.String("imagor-prefilter-depthmap-api", "",
			"API URL for depthmap prefilter")
		imagorPrefilterDepthmapTimeout = fs.Duration("imagor-prefilter-depthmap-timeout",
			60*time.Second, "Timeout for depthmap prefilter API calls")
		imagorPrefilterRemovebgAPI = fs.String("imagor-prefilter-removebg-api", "",
			"API URL for removebg prefilter")
		imagorPrefilterRemovebgTimeout = fs.Duration("imagor-prefilter-removebg-timeout",
			60*time.Second, "Timeout for removebg prefilter API calls")

		options, logger, isDebug = applyOptions(fs, cb, append(funcs, baseConfig...)...)

		alg          = sha1.New
		hasher       imagorpath.StorageHasher
		resultHasher imagorpath.ResultStorageHasher
	)

	if strings.ToLower(*imagorSignerType) == "sha256" {
		alg = sha256.New
	} else if strings.ToLower(*imagorSignerType) == "sha512" {
		alg = sha512.New
	}

	if strings.ToLower(*imagorStoragePathStyle) == "digest" {
		hasher = imagorpath.DigestStorageHasher
	}

	if strings.ToLower(*imagorResultStoragePathStyle) == "digest" {
		resultHasher = imagorpath.DigestResultStorageHasher
	} else if strings.ToLower(*imagorResultStoragePathStyle) == "suffix" {
		resultHasher = imagorpath.SuffixResultStorageHasher
	} else if strings.ToLower(*imagorResultStoragePathStyle) == "size" {
		resultHasher = imagorpath.SizeSuffixResultStorageHasher
	}

	return imagor.New(append(
		options,
		imagor.WithSigner(imagorpath.NewHMACSigner(
			alg, *imagorSignerTruncate, *imagorSecret,
		)),
		imagor.WithBasePathRedirect(*imagorBasePathRedirect),
		imagor.WithBaseParams(*imagorBaseParams),
		imagor.WithRequestTimeout(*imagorRequestTimeout),
		imagor.WithLoadTimeout(*imagorLoadTimeout),
		imagor.WithSaveTimeout(*imagorSaveTimeout),
		imagor.WithProcessTimeout(*imagorProcessTimeout),
		imagor.WithProcessConcurrency(*imagorProcessConcurrency),
		imagor.WithProcessQueueSize(*imagorProcessQueueSize),
		imagor.WithCacheHeaderTTL(*imagorCacheHeaderTTL),
		imagor.WithCacheHeaderSWR(*imagorCacheHeaderSWR),
		imagor.WithCacheHeaderNoCache(*imagorCacheHeaderNoCache),
		imagor.WithAutoWebP(*imagorAutoWebP),
		imagor.WithAutoAVIF(*imagorAutoAVIF),
		imagor.WithModifiedTimeCheck(*imagorModifiedTimeCheck),
		imagor.WithDisableErrorBody(*imagorDisableErrorBody),
		imagor.WithDisableParamsEndpoint(*imagorDisableParamsEndpoint),
		imagor.WithStoragePathStyle(hasher),
		imagor.WithResultStoragePathStyle(resultHasher),
		imagor.WithUnsafe(*imagorUnsafe),
		withPrefiltersOption(*imagorPrefilterDepthmapAPI, *imagorPrefilterDepthmapTimeout,
			*imagorPrefilterRemovebgAPI, *imagorPrefilterRemovebgTimeout, logger),
		imagor.WithLogger(logger),
		imagor.WithDebug(isDebug),
	)...)
}

// CreateServer create server from config flags. Returns nil on version or help command
func CreateServer(args []string, funcs ...Option) (srv *server.Server) {
	var (
		fs     = flag.NewFlagSet("imagor", flag.ExitOnError)
		logger *zap.Logger
		err    error
		app    *imagor.Imagor

		debug        = fs.Bool("debug", false, "Debug mode")
		version      = fs.Bool("version", false, "imagor version")
		port         = fs.Int("port", 8000, "Server port")
		goMaxProcess = fs.Int("gomaxprocs", 0, "GOMAXPROCS")

		bind = fs.String("bind", "",
			"Server address and port to bind .e.g. myhost:8888. This overrides server address and port config")

		_ = fs.String("config", ".env", "Retrieve configuration from the given file")

		serverAddress = fs.String("server-address", "",
			"Server address")
		serverPathPrefix = fs.String("server-path-prefix", "",
			"Server path prefix")
		serverCORS = fs.Bool("server-cors", false,
			"Enable CORS")
		serverStripQueryString = fs.Bool("server-strip-query-string", false,
			"Enable strip query string redirection")
		serverAccessLog = fs.Bool("server-access-log", false,
			"Enable server access log")

		prometheusBind = fs.String("prometheus-bind", "", "Specify address and port to enable Prometheus metrics, e.g. :5000, prom:7000")
		prometheusPath = fs.String("prometheus-path", "/", "Prometheus metrics path")
	)

	app = NewImagor(fs, func() (*zap.Logger, bool) {
		if err = ff.Parse(fs, args,
			ff.WithEnvVars(),
			ff.WithConfigFileFlag("config"),
			ff.WithIgnoreUndefined(true),
			ff.WithAllowMissingConfigFile(true),
			ff.WithConfigFileParser(ff.EnvParser),
		); err != nil {
			panic(err)
		}
		if *debug {
			logger = zap.Must(zap.NewDevelopment())
		} else {
			logger = zap.Must(zap.NewProduction())
		}
		return logger, *debug
	}, funcs...)

	if *version {
		fmt.Println(imagor.Version)
		return
	}

	if *goMaxProcess > 0 {
		logger.Debug("GOMAXPROCS", zap.Int("count", *goMaxProcess))
		runtime.GOMAXPROCS(*goMaxProcess)
	}

	var pm *prometheusmetrics.PrometheusMetrics
	if *prometheusBind != "" {
		pm = prometheusmetrics.New(
			prometheusmetrics.WithAddr(*prometheusBind),
			prometheusmetrics.WithPath(*prometheusPath),
			prometheusmetrics.WithLogger(logger),
		)
	}

	return server.New(app,
		server.WithAddr(*bind),
		server.WithPort(*port),
		server.WithAddress(*serverAddress),
		server.WithPathPrefix(*serverPathPrefix),
		server.WithCORS(*serverCORS),
		server.WithStripQueryString(*serverStripQueryString),
		server.WithAccessLog(*serverAccessLog),
		server.WithLogger(logger),
		server.WithDebug(*debug),
		server.WithMetrics(pm),
	)
}

// withPrefiltersOption creates an option to initialize prefilters based on configuration
func withPrefiltersOption(depthmapURL string, depthmapTimeout time.Duration, removebgURL string, removebgTimeout time.Duration, logger *zap.Logger) imagor.Option {
	return func(app *imagor.Imagor) {
		// Initialize depthmap prefilter if API URL is configured
		if depthmapURL != "" {
			depthmapPrefilter := imagor.NewDepthmapPrefilter(depthmapURL, depthmapTimeout)
			app.Prefilters = append(app.Prefilters, depthmapPrefilter)
			logger.Info("initialized depthmap prefilter",
				zap.String("api_url", depthmapURL),
				zap.Duration("timeout", depthmapTimeout))
		}

		// Initialize removebg prefilter if API URL is configured
		if removebgURL != "" {
			removebgPrefilter := imagor.NewRemoveBgPrefilter(removebgURL, removebgTimeout)
			app.Prefilters = append(app.Prefilters, removebgPrefilter)
			logger.Info("initialized removebg prefilter",
				zap.String("api_url", removebgURL),
				zap.Duration("timeout", removebgTimeout))
		}
	}
}

// withPrefilterStorage configures prefilter storage based on configuration
func withPrefilterStorage(fs *flag.FlagSet, cb func() (*zap.Logger, bool)) imagor.Option {
	var (
		fileSafeChars = fs.Lookup("file-safe-chars")

		filePrefilterStorageBaseDir = fs.String("file-prefilter-storage-base-dir", "",
			"Base directory for File Prefilter Storage. Enable File Prefilter Storage only if this value present")
		filePrefilterStoragePathPrefix = fs.String("file-prefilter-storage-path-prefix", "",
			"Base path prefix for File Prefilter Storage")
		filePrefilterStorageMkdirPermission = fs.String("file-prefilter-storage-mkdir-permission", "0755",
			"File Prefilter Storage mkdir permission")
		filePrefilterStorageWritePermission = fs.String("file-prefilter-storage-write-permission", "0666",
			"File Prefilter Storage write permission")
		filePrefilterStorageExpiration = fs.Duration("file-prefilter-storage-expiration", 0,
			"File Prefilter Storage expiration duration e.g. 24h. Default no expiration")
		filePrefilterStoragePathStyle = fs.String("file-prefilter-storage-path-style", "digest",
			"Prefilter storage path style: original, digest. Default digest")

		logger, _ = cb()
	)

	return func(app *imagor.Imagor) {
		// Configure prefilter storage path style
		if strings.ToLower(*filePrefilterStoragePathStyle) == "digest" {
			app.PrefilterStoragePathStyle = imagorpath.DigestPrefilterStorageHasher
		}

		var safeChars string
		if fileSafeChars != nil {
			safeChars = fileSafeChars.Value.String()
		}

		if *filePrefilterStorageBaseDir != "" {
			// activate File Prefilter Storage only if base dir config presents
			app.PrefilterStorages = append(app.PrefilterStorages,
				filestorage.New(
					*filePrefilterStorageBaseDir,
					filestorage.WithPathPrefix(*filePrefilterStoragePathPrefix),
					filestorage.WithMkdirPermission(*filePrefilterStorageMkdirPermission),
					filestorage.WithWritePermission(*filePrefilterStorageWritePermission),
					filestorage.WithSafeChars(safeChars),
					filestorage.WithExpiration(*filePrefilterStorageExpiration),
				),
			)
			logger.Info("prefilter storage enabled",
				zap.String("type", "file"),
				zap.String("base_dir", *filePrefilterStorageBaseDir))
		}
	}
}
