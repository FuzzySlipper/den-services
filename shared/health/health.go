package health

import (
	"net/http"
	"time"

	"den-services/shared/api"
)

type BuildInfo struct {
	ServiceName string    `json:"service_name"`
	Version     string    `json:"version"`
	Commit      string    `json:"commit"`
	BuiltAt     time.Time `json:"built_at"`
}

func NewBuildInfo(serviceName string, version string, commit string, builtAt time.Time) (BuildInfo, error) {
	info := BuildInfo{
		ServiceName: serviceName,
		Version:     version,
		Commit:      commit,
		BuiltAt:     builtAt.UTC(),
	}
	if err := info.Validate(); err != nil {
		return BuildInfo{}, err
	}
	return info, nil
}

func (b BuildInfo) Validate() error {
	if b.ServiceName == "" {
		return ErrMissingServiceName
	}
	if b.Version == "" {
		return ErrMissingVersion
	}
	if b.Commit == "" {
		return ErrMissingCommit
	}
	if b.BuiltAt.IsZero() {
		return ErrMissingBuiltAt
	}
	return nil
}

type HealthResponse struct {
	Status      string    `json:"status"`
	ServiceName string    `json:"service_name"`
	Version     string    `json:"version"`
	Commit      string    `json:"commit"`
	BuiltAt     time.Time `json:"built_at"`
}

type VersionResponse struct {
	ServiceName string    `json:"service_name"`
	Version     string    `json:"version"`
	Commit      string    `json:"commit"`
	BuiltAt     time.Time `json:"built_at"`
}

func HealthHandler(info BuildInfo) (http.Handler, error) {
	if err := info.Validate(); err != nil {
		return nil, err
	}

	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		api.WriteJSON(w, http.StatusOK, HealthResponse{
			Status:      "ok",
			ServiceName: info.ServiceName,
			Version:     info.Version,
			Commit:      info.Commit,
			BuiltAt:     info.BuiltAt,
		})
	}), nil
}

func VersionHandler(info BuildInfo) (http.Handler, error) {
	if err := info.Validate(); err != nil {
		return nil, err
	}

	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		api.WriteJSON(w, http.StatusOK, VersionResponse{
			ServiceName: info.ServiceName,
			Version:     info.Version,
			Commit:      info.Commit,
			BuiltAt:     info.BuiltAt,
		})
	}), nil
}
