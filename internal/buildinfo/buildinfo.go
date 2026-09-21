package buildinfo

import (
	"fmt"
	"runtime"
)

var (
	version  = "0.0.0-dev"
	revision = "unknown"
	created  = "unknown"
)

type Info struct {
	Version          string
	Revision         string
	Created          string
	GoVersion        string
	SupportedEngines string
}

func Current() Info {
	return Info{
		Version:          version,
		Revision:         revision,
		Created:          created,
		GoVersion:        runtime.Version(),
		SupportedEngines: "mihomo/1.19.31,sing-box/1.14.1",
	}
}

func (info Info) String() string {
	return fmt.Sprintf("egressfox-operator %s revision=%s created=%s go=%s engines=%s",
		info.Version, info.Revision, info.Created, info.GoVersion, info.SupportedEngines)
}
