package app

import (
	"flag"
	"fmt"
	"runtime/debug"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods"
)

const AppCommonName = "mod-bisect-tool"
const AppGuiName = "mod-bisect-gui"
const AppTuiName = "mod-bisect-tui"

const (
	AppDistributionDevelopment   = "development"
	AppDistributionWindowsBinary = "windows-binary"
	AppDistributionLinuxBinary   = "linux-binary"
	AppDistributionLinuxAppImage = "linux-appimage"
	AppDistributionDarwinBinary  = "darwin-binary"
	AppDistributionDarwinApp     = "darwin-app"
)

var AppDistribution = AppDistributionDevelopment

var (
	AppVersion   = "dev"
	AppRevision  = "unknown"
	AppBuildTime = "unknown"
)

func init() {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.time":
				AppBuildTime = setting.Value
			case "vcs.revision":
				AppRevision = setting.Value
			}
		}
	}
	if AppRevision == "" {
		AppRevision = "unknown"
	}
	if AppBuildTime == "" {
		AppBuildTime = "unknown"
	}
}

func shortRevision(rev string) string {
	if rev == "" || rev == "unknown" {
		return "unknown"
	}
	if len(rev) <= 12 {
		return rev
	}
	return rev[:12]
}

func VersionText() string {
	if AppVersion != "" && AppVersion != "dev" {
		if AppRevision != "" && AppRevision != "unknown" {
			return fmt.Sprintf("%s (%s)", AppVersion, shortRevision(AppRevision))
		}
		return AppVersion
	}
	if AppRevision != "" && AppRevision != "unknown" {
		return shortRevision(AppRevision)
	}
	return "unknown"
}

// StartupInfo returns a compact startup message with build metadata and the
// distribution type.
func StartupInfo() string {
	return fmt.Sprintf("Main: Starting %s (distribution=%s, version=%s, revision=%s, build_time=%s)", AppCommonName, AppDistribution, AppVersion, AppRevision, AppBuildTime)
}

// CLIArgs holds all command-line arguments passed to the application.
type CLIArgs struct {
	NoEmbeddedOverrides     bool
	NoLogFile               bool
	Verbose                 bool
	Loader                  mods.RunLoader
	LogDir                  string
	Locale                  string
	AdditionalOverridesPath string
}

// ParseCLIArgs parses the command-line flags and returns a populated CLIArgs struct.
func ParseCLIArgs() *CLIArgs {
	args := &CLIArgs{}

	flag.BoolVar(&args.NoEmbeddedOverrides, "no-embedded-overrides", false, "Disable the built-in dependency overrides for known problematic mods.")
	flag.BoolVar(&args.NoLogFile, "no-log-file", false, "Disable logging to a file; log output stays in memory only.")
	flag.BoolVar(&args.Verbose, "verbose", false, "Enable verbose (debug) logging.")
	flag.Func("loader", "Mod loader to run with: fabric, neoforge, connector (NeoForge with Fabric) or kilt (Fabric with NeoForge). Defaults to auto-detection.", func(value string) error {
		loader, err := mods.ParseRunLoader(value)
		if err != nil {
			return err
		}
		args.Loader = loader
		return nil
	})
	flag.StringVar(&args.LogDir, "log-dir", "", "Specifies the directory to store log files.")
	flag.StringVar(&args.AdditionalOverridesPath, "dependency-overrides", "", "Path to an additional dependency overrides JSON file.")
	flag.StringVar(&args.Locale, "locale", "", "GUI language/locale. Defaults to the system locale.")
	flag.Parse()

	return args
}
