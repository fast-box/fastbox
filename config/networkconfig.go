// Copyright 2018 The sphinx Authors
// Modified based on go-ethereum, which Copyright (C) 2014 The go-ethereum Authors.
//
// The sphinx is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The sphinx is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the sphinx. If not, see <http://www.gnu.org/licenses/>.

package config

import (
	"fmt"
	"github.com/shx-project/sphinx/network/p2p/discover"
	"github.com/shx-project/sphinx/network/p2p/nat"
	"github.com/shx-project/sphinx/network/p2p/netutil"

	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultHTTPHost = "localhost" // Default host interface for the HTTP RPC server
	DefaultHTTPPort = 8600        // Default TCP port for the HTTP RPC server
	DefaultWSHost   = "localhost" // Default host interface for the websocket RPC server
	DefaultWSPort   = 8601        // Default TCP port for the websocket RPC server
)

const (
	clientIdentifier = "shx" // Client identifier to advertise over the network
)
const (
	// BloomBitsBlocks is the number of blocks a single bloom bit section vector
	// contains.
	BloomBitsBlocks uint64 = 4096
)

var DefaultNTConfig = NetworkConfig{

	HTTPPort:         DefaultHTTPPort,
	HTTPModules:      []string{"net", "web3", "prometheus"},
	WSPort:           DefaultWSPort,
	WSModules:        []string{"net", "web3", "prometheus"},
	HTTPVirtualHosts: []string{"localhost"},
	HTTPTimeouts:     DefaultHTTPTimeouts,
	ListenAddr:       ":27000",
	MaxPeers:         50,
	NAT:              nat.Any(),
	IpcEndpoint:      DefaultIPCEndpoint(clientIdentifier),
	HttpEndpoint:     DefaultHTTPEndpoint(),
	WsEndpoint:       DefaultWSEndpoint(),
}

var MainnetBootnodes = []string{
	"hnode://7644221a9e46dab6439e001ed2b2a6b30cc98eff7ff398cadf64d5ab0552bcc0d58e0391b90775c7f3c87e81e17b9482290df79f4f7c95047830ac64bfb6d65e@221.194.47.245:27000",
}

// TestnetBootnodes are the hnode URLs of the P2P bootstrap nodes running on the
// Ropsten test network.
var TestnetBootnodes = []string{}

type NetworkConfig struct {
	// HTTPHost is the host interface on which to start the HTTP RPC server. If this
	// field is empty, no HTTP API endpoint will be started.
	HTTPHost string `toml:",omitempty"`

	// HTTPPort is the TCP port number on which to start the HTTP RPC server. The
	// default zero value is/ valid and will pick a port number randomly (useful
	// for ephemeral nodes).
	HTTPPort int `toml:",omitempty"`

	// HTTPCors is the Cross-Origin Resource Sharing header to send to requesting
	// clients. Please be aware that CORS is a browser enforced security, it's fully
	// useless for custom HTTP clients.
	HTTPCors []string `toml:",omitempty"`

	// HTTPVirtualHosts is the list of virtual hostnames which are allowed on incoming requests.
	// This is by default {'localhost'}. Using this prevents attacks like
	// DNS rebinding, which bypasses SOP by simply masquerading as being within the same
	// origin. These attacks do not utilize CORS, since they are not cross-domain.
	// By explicitly checking the Host-header, the server will not allow requests
	// made against the server with a malicious host domain.
	// Requests using ip address directly are not affected
	HTTPVirtualHosts []string `toml:",omitempty"`

	// HTTPModules is a list of API modules to expose via the HTTP RPC interface.
	// If the module list is empty, all RPC API endpoints designated public will be
	// exposed.
	HTTPModules []string `toml:",omitempty"`

	// HTTPTimeouts allows for customization of the timeout values used by the HTTP RPC
	// interface.
	HTTPTimeouts HTTPTimeouts

	// WSHost is the host interface on which to start the websocket RPC server. If
	// this field is empty, no websocket API endpoint will be started.
	WSHost string `toml:",omitempty"`

	// WSPort is the TCP port number on which to start the websocket RPC server. The
	// default zero value is/ valid and will pick a port number randomly (useful for
	// ephemeral nodes).
	WSPort int `toml:",omitempty"`

	// WSOrigins is the list of domain to accept websocket requests from. Please be
	// aware that the server can only act upon the HTTP request the client sends and
	// cannot verify the validity of the request header.
	WSOrigins []string `toml:",omitempty"`

	// WSModules is a list of API modules to expose via the websocket RPC interface.
	// If the module list is empty, all RPC API endpoints designated public will be
	// exposed.
	WSModules []string `toml:",omitempty"`

	// WSExposeAll exposes all API modules via the WebSocket RPC interface rather
	// than just the public ones.
	//
	// *WARNING* Only set this if the node is running in a trusted network, exposing
	// private APIs to untrusted users is a major security risk.
	WSExposeAll bool `toml:",omitempty"`

	// MaxPeers is the maximum number of peers that can be
	// connected. It must be greater than zero.
	MaxPeers int

	// MaxPendingPeers is the maximum number of peers that can be pending in the
	// handshake phase, counted separately for inbound and outbound connections.
	// Zero defaults to preset values.
	MaxPendingPeers int `toml:",omitempty"`

	// DiscoveryV5 specifies whether the the new topic-discovery based V5 discovery
	// protocol should be started or not.
	//DiscoveryV5 bool `toml:",omitempty"`
	NoDiscovery bool
	// Listener address for the V5 discovery protocol UDP traffic.
	//DiscoveryV5Addr string `toml:",omitempty"`

	// Name sets the node name of this server.
	// Use common.MakeName to create a name that follows existing conventions.
	Name string `toml:"-"`

	// RoleType sets the node type of this server.
	// One of hpnode,prenode,access,light.
	RoleType string

	// Connectivity can be restricted to certain IP networks.
	// If this option is set to a non-nil value, only hosts which match one of the
	// IP networks contained in the list are considered.
	NetRestrict *netutil.Netlist `toml:",omitempty"`

	// NodeDatabase is the path to the database containing the previously seen
	// live nodes in the network.
	NodeDatabase string `toml:",omitempty"`

	// If ListenAddr is set to a non-nil address, the server
	// will listen for incoming connections.
	//
	// If the port is zero, the operating system will pick a port. The
	// ListenAddr field will be updated with the actual address when
	// the server is started.
	ListenAddr string

	// If set to a non-nil value, the given NAT port mapper
	// is used to make the listening port available to the
	// Internet.
	NAT nat.Interface `toml:",omitempty"`

	// If NoDial is true, the server will not dial any peers.
	NoDial bool `toml:",omitempty"`

	// If EnableMsgEvents is set then the server will emit PeerEvents
	// whenever a message is sent to or received from a peer
	EnableMsgEvents bool

	IpcEndpoint  string // IPC endpoint to listen at (empty = IPC disabled)
	HttpEndpoint string // HTTP endpoint (interface + port) to listen at (empty = HTTP disabled)
	WsEndpoint   string // Websocket endpoint (interface + port) to listen at (empty = websocket disabled)

	BootstrapNodes []*discover.Node
}

// HTTPTimeouts represents the configuration params for the HTTP RPC server.
type HTTPTimeouts struct {
	// ReadTimeout is the maximum duration for reading the entire
	// request, including the body.
	//
	// Because ReadTimeout does not let Handlers make per-request
	// decisions on each request body's acceptable deadline or
	// upload rate, most users will prefer to use
	// ReadHeaderTimeout. It is valid to use them both.
	ReadTimeout time.Duration

	// WriteTimeout is the maximum duration before timing out
	// writes of the response. It is reset whenever a new
	// request's header is read. Like ReadTimeout, it does not
	// let Handlers make decisions on a per-request basis.
	WriteTimeout time.Duration

	// IdleTimeout is the maximum amount of time to wait for the
	// next request when keep-alives are enabled. If IdleTimeout
	// is zero, the value of ReadTimeout is used. If both are
	// zero, ReadHeaderTimeout is used.
	IdleTimeout time.Duration
}

// DefaultHTTPTimeouts represents the default timeout values used if further
// configuration is not provided.
var DefaultHTTPTimeouts = HTTPTimeouts{
	ReadTimeout:  30 * time.Second,
	WriteTimeout: 30 * time.Second,
	IdleTimeout:  120 * time.Second,
}

func DefaultNetworkConfig() NetworkConfig {
	cfg := DefaultNTConfig

	cfg.HTTPModules = append(cfg.HTTPModules, "shx")
	cfg.WSModules = append(cfg.WSModules, "shx")

	cfg.HTTPVirtualHosts = append(cfg.HTTPVirtualHosts, "localhost")
	cfg.HTTPTimeouts = DefaultHTTPTimeouts

	return cfg
}

// HTTPEndpoint resolves an HTTP endpoint based on the configured host interface
// and port parameters.
func (c *NetworkConfig) HTTPEndpoint() string {
	if c.HTTPHost == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d", c.HTTPHost, c.HTTPPort)
}

// DefaultHTTPEndpoint returns the HTTP endpoint used by default.
func DefaultHTTPEndpoint() string {
	config := &NetworkConfig{HTTPHost: DefaultHTTPHost, HTTPPort: DefaultHTTPPort}
	return config.HTTPEndpoint()
}

// DefaultIPCEndpoint returns the IPC path used by default.
func DefaultIPCEndpoint(clientIdentifier string) string {
	if clientIdentifier == "" {
		clientIdentifier = strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
		if clientIdentifier == "" {
			panic("empty executable name")
		}
	}
	config := &Nodeconfig{DataDir: DefaultDataDir(), IPCPath: clientIdentifier + ".ipc"}
	return config.IPCEndpoint()
}

// WSEndpoint resolves an websocket endpoint based on the configured host interface
// and port parameters.
func (c *NetworkConfig) WSEndpoint() string {
	if c.WSHost == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d", c.WSHost, c.WSPort)
}

// DefaultWSEndpoint returns the websocket endpoint used by default.
func DefaultWSEndpoint() string {
	config := &NetworkConfig{WSHost: DefaultWSHost, WSPort: DefaultWSPort}
	return config.WSEndpoint()
}
