package main

import (
	"fmt"
	"net"
	"net/url"
	"strconv"

	"github.com/urfave/cli"
)

// Config contains the configuration passed on the command line.
type Config struct {
	ListenAddress     string `json:"listenAddress"`
	UpstreamHost      string `json:"upstreamHost"`
	UpstreamPort      int    `json:"upstreamPort"`
	UpstreamSocket    string `json:"upstreamSocket"`
	APIPath           string `json:"apiPath"`
	CASURL            string `json:"casUrl"`
	Scope             string `json:"scope"`
	ValidationTimeout int    `json:"validationTimeout"`
	ValidationEnabled bool   `json:"validationEnabled"`
	CASInsecure       bool   `json:"casInsecure"`
}

func createGlobalFlags() []cli.Flag {
	return []cli.Flag{
		cli.StringFlag{Name: "listenAddress, la", Usage: "address to listen on", Value: "0.0.0.0:8080", EnvVar: "PAT_PROXY_LISTEN_ADDRESS"},
		cli.StringFlag{Name: "upstreamHost, uh", Usage: "hostname of the upstream server", Value: "127.0.0.1", EnvVar: "PAT_PROXY_UPSTREAM_HOST"},
		cli.IntFlag{Name: "upstreamPort, up", Usage: "port of the upstream server", Value: 8081, EnvVar: "PAT_PROXY_UPSTREAM_PORT"},
		cli.StringFlag{Name: "upstreamSocket, us", Usage: "path to the upstream Unix socket", EnvVar: "PAT_PROXY_UPSTREAM_SOCKET"},
		cli.StringFlag{Name: "apiPath, api", Usage: "path of the API on dogu to check PAT for e.g. /teamscale/api", Value: "/", EnvVar: "PAT_PROXY_API_PATH"},
		cli.StringFlag{Name: "casUrl, cas", Usage: "full CAS PAT validation URL", EnvVar: "PAT_PROXY_CAS_URL"},
		cli.StringFlag{Name: "scope, s", Usage: "scope for CAS PAT validation", EnvVar: "PAT_PROXY_SCOPE"},
		cli.IntFlag{Name: "validationTimeout, t", Usage: "validation timeout in seconds", Value: 5},
		cli.BoolTFlag{Name: "validationEnabled", Usage: "enable CAS PAT validation", EnvVar: "PAT_PROXY_VALIDATION_ENABLED"},
		cli.BoolFlag{Name: "casInsecure", Usage: "disable certificate verification for CAS", EnvVar: "PAT_PROXY_CAS_INSECURE"},
	}
}

func readCLIConfig(c *cli.Context) Config {
	return Config{
		ListenAddress:     c.String("listenAddress"),
		UpstreamHost:      c.String("upstreamHost"),
		UpstreamPort:      c.Int("upstreamPort"),
		UpstreamSocket:    c.String("upstreamSocket"),
		APIPath:           c.String("apiPath"),
		CASURL:            c.String("casUrl"),
		Scope:             c.String("scope"),
		ValidationTimeout: c.Int("validationTimeout"),
		ValidationEnabled: c.BoolT("validationEnabled"),
		CASInsecure:       c.Bool("casInsecure"),
	}
}

func (c Config) validate() error {
	if c.ValidationTimeout <= 0 {
		return fmt.Errorf("validation timeout must be positive")
	}
	if c.APIPath == "" || c.APIPath[0] != '/' {
		return fmt.Errorf("PAT_PROXY_API_PATH must start with /")
	}
	if c.ValidationEnabled {
		u, err := url.Parse(c.CASURL)
		if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
			return fmt.Errorf("PAT_PROXY_CAS_URL must be an HTTP(S) URL without embedded credentials or fragment")
		}
	}
	_, listenPort, err := net.SplitHostPort(c.ListenAddress)
	if err != nil {
		return fmt.Errorf("listen address must contain a host and port")
	}
	port, err := strconv.Atoi(listenPort)
	if err != nil || port < 1 || port > 65535 || c.UpstreamPort < 1 || c.UpstreamPort > 65535 {
		return fmt.Errorf("listen and upstream ports must be between 1 and 65535")
	}
	return nil
}