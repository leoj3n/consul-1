package config

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul/agent/checks"
	"github.com/hashicorp/consul/agent/structs"
	"github.com/hashicorp/consul/lib"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/consul/types"
	"github.com/pascaldekloe/goe/verify"
	"github.com/stretchr/testify/require"
)

type configTest struct {
	desc           string
	args           []string
	pre, post      func()
	json, jsontail []string
	hcl, hcltail   []string
	skipformat     bool
	privatev4      func() ([]*net.IPAddr, error)
	publicv6       func() ([]*net.IPAddr, error)
	patch          func(rt *RuntimeConfig)
	err            string
	warns          []string
	hostname       func() (string, error)
}

// TestConfigFlagsAndEdgecases tests the command line flags and
// edgecases for the config parsing. It provides a test structure which
// checks for warnings on deprecated fields and flags.  These tests
// should check one option at a time if possible and should use generic
// values, e.g. 'a' or 1 instead of 'servicex' or 3306.

func TestConfigFlagsAndEdgecases(t *testing.T) {
	dataDir := testutil.TempDir(t, "consul")
	defer os.RemoveAll(dataDir)

	tests := []configTest{
		// ------------------------------------------------------------
		// cmd line flags
		//

		{
			desc: "-advertise",
			args: []string{
				`-advertise=1.2.3.4`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.AdvertiseAddrLAN = ipAddr("1.2.3.4")
				rt.AdvertiseAddrWAN = ipAddr("1.2.3.4")
				rt.RPCAdvertiseAddr = tcpAddr("1.2.3.4:8300")
				rt.SerfAdvertiseAddrLAN = tcpAddr("1.2.3.4:8301")
				rt.SerfAdvertiseAddrWAN = tcpAddr("1.2.3.4:8302")
				rt.TaggedAddresses = map[string]string{
					"lan": "1.2.3.4",
					"wan": "1.2.3.4",
				}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-advertise-wan",
			args: []string{
				`-advertise-wan=1.2.3.4`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.AdvertiseAddrWAN = ipAddr("1.2.3.4")
				rt.SerfAdvertiseAddrWAN = tcpAddr("1.2.3.4:8302")
				rt.TaggedAddresses = map[string]string{
					"lan": "10.0.0.1",
					"wan": "1.2.3.4",
				}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-advertise and -advertise-wan",
			args: []string{
				`-advertise=1.2.3.4`,
				`-advertise-wan=5.6.7.8`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.AdvertiseAddrLAN = ipAddr("1.2.3.4")
				rt.AdvertiseAddrWAN = ipAddr("5.6.7.8")
				rt.RPCAdvertiseAddr = tcpAddr("1.2.3.4:8300")
				rt.SerfAdvertiseAddrLAN = tcpAddr("1.2.3.4:8301")
				rt.SerfAdvertiseAddrWAN = tcpAddr("5.6.7.8:8302")
				rt.TaggedAddresses = map[string]string{
					"lan": "1.2.3.4",
					"wan": "5.6.7.8",
				}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-bind",
			args: []string{
				`-bind=1.2.3.4`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.BindAddr = ipAddr("1.2.3.4")
				rt.AdvertiseAddrLAN = ipAddr("1.2.3.4")
				rt.AdvertiseAddrWAN = ipAddr("1.2.3.4")
				rt.RPCAdvertiseAddr = tcpAddr("1.2.3.4:8300")
				rt.RPCBindAddr = tcpAddr("1.2.3.4:8300")
				rt.SerfAdvertiseAddrLAN = tcpAddr("1.2.3.4:8301")
				rt.SerfAdvertiseAddrWAN = tcpAddr("1.2.3.4:8302")
				rt.SerfBindAddrLAN = tcpAddr("1.2.3.4:8301")
				rt.SerfBindAddrWAN = tcpAddr("1.2.3.4:8302")
				rt.TaggedAddresses = map[string]string{
					"lan": "1.2.3.4",
					"wan": "1.2.3.4",
				}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-bootstrap",
			args: []string{
				`-bootstrap`,
				`-server`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.Bootstrap = true
				rt.ServerMode = true
				rt.LeaveOnTerm = false
				rt.SkipLeaveOnInt = true
				rt.DataDir = dataDir
			},
			warns: []string{"bootstrap = true: do not enable unless necessary"},
		},
		{
			desc: "-bootstrap-expect",
			args: []string{
				`-bootstrap-expect=3`,
				`-server`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.BootstrapExpect = 3
				rt.ServerMode = true
				rt.LeaveOnTerm = false
				rt.SkipLeaveOnInt = true
				rt.DataDir = dataDir
			},
			warns: []string{"bootstrap_expect > 0: expecting 3 servers"},
		},
		{
			desc: "-client",
			args: []string{
				`-client=1.2.3.4`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.ClientAddrs = []*net.IPAddr{ipAddr("1.2.3.4")}
				rt.DNSAddrs = []net.Addr{tcpAddr("1.2.3.4:8600"), udpAddr("1.2.3.4:8600")}
				rt.HTTPAddrs = []net.Addr{tcpAddr("1.2.3.4:8500")}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-config-dir",
			args: []string{
				`-data-dir=` + dataDir,
				`-config-dir`, filepath.Join(dataDir, "conf.d"),
			},
			patch: func(rt *RuntimeConfig) {
				rt.Datacenter = "a"
				rt.DataDir = dataDir
			},
			pre: func() {
				writeFile(filepath.Join(dataDir, "conf.d/conf.json"), []byte(`{"datacenter":"a"}`))
			},
		},
		{
			desc: "-config-file json",
			args: []string{
				`-data-dir=` + dataDir,
				`-config-file`, filepath.Join(dataDir, "conf.json"),
			},
			patch: func(rt *RuntimeConfig) {
				rt.Datacenter = "a"
				rt.DataDir = dataDir
			},
			pre: func() {
				writeFile(filepath.Join(dataDir, "conf.json"), []byte(`{"datacenter":"a"}`))
			},
		},
		{
			desc: "-config-file hcl and json",
			args: []string{
				`-data-dir=` + dataDir,
				`-config-file`, filepath.Join(dataDir, "conf.hcl"),
				`-config-file`, filepath.Join(dataDir, "conf.json"),
			},
			patch: func(rt *RuntimeConfig) {
				rt.Datacenter = "b"
				rt.DataDir = dataDir
			},
			pre: func() {
				writeFile(filepath.Join(dataDir, "conf.hcl"), []byte(`datacenter = "a"`))
				writeFile(filepath.Join(dataDir, "conf.json"), []byte(`{"datacenter":"b"}`))
			},
		},
		{
			desc: "-data-dir empty",
			args: []string{
				`-data-dir=`,
			},
			err: "data_dir cannot be empty",
		},
		{
			desc: "-data-dir non-directory",
			args: []string{
				`-data-dir=runtime_test.go`,
			},
			err: `data_dir "runtime_test.go" is not a directory`,
		},
		{
			desc: "-datacenter",
			args: []string{
				`-datacenter=a`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.Datacenter = "a"
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-datacenter empty",
			args: []string{
				`-datacenter=`,
				`-data-dir=` + dataDir,
			},
			err: "datacenter cannot be empty",
		},
		{
			desc: "-dev",
			args: []string{
				`-dev`,
			},
			patch: func(rt *RuntimeConfig) {
				rt.AdvertiseAddrLAN = ipAddr("127.0.0.1")
				rt.AdvertiseAddrWAN = ipAddr("127.0.0.1")
				rt.BindAddr = ipAddr("127.0.0.1")
				rt.ConnectEnabled = true
				rt.DevMode = true
				rt.DisableAnonymousSignature = true
				rt.DisableKeyringFile = true
				rt.EnableDebug = true
				rt.EnableUI = true
				rt.LeaveOnTerm = false
				rt.LogLevel = "DEBUG"
				rt.RPCAdvertiseAddr = tcpAddr("127.0.0.1:8300")
				rt.RPCBindAddr = tcpAddr("127.0.0.1:8300")
				rt.SerfAdvertiseAddrLAN = tcpAddr("127.0.0.1:8301")
				rt.SerfAdvertiseAddrWAN = tcpAddr("127.0.0.1:8302")
				rt.SerfBindAddrLAN = tcpAddr("127.0.0.1:8301")
				rt.SerfBindAddrWAN = tcpAddr("127.0.0.1:8302")
				rt.ServerMode = true
				rt.SkipLeaveOnInt = true
				rt.TaggedAddresses = map[string]string{"lan": "127.0.0.1", "wan": "127.0.0.1"}
				rt.ConsulCoordinateUpdatePeriod = 100 * time.Millisecond
				rt.ConsulRaftElectionTimeout = 52 * time.Millisecond
				rt.ConsulRaftHeartbeatTimeout = 35 * time.Millisecond
				rt.ConsulRaftLeaderLeaseTimeout = 20 * time.Millisecond
				rt.GossipLANGossipInterval = 100 * time.Millisecond
				rt.GossipLANProbeInterval = 100 * time.Millisecond
				rt.GossipLANProbeTimeout = 100 * time.Millisecond
				rt.GossipLANSuspicionMult = 3
				rt.GossipWANGossipInterval = 100 * time.Millisecond
				rt.GossipWANProbeInterval = 100 * time.Millisecond
				rt.GossipWANProbeTimeout = 100 * time.Millisecond
				rt.GossipWANSuspicionMult = 3
				rt.ConsulServerHealthInterval = 10 * time.Millisecond
				rt.GRPCPort = 8502
				rt.GRPCAddrs = []net.Addr{tcpAddr("127.0.0.1:8502")}
			},
		},
		{
			desc: "-disable-host-node-id",
			args: []string{
				`-disable-host-node-id`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.DisableHostNodeID = true
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-disable-keyring-file",
			args: []string{
				`-disable-keyring-file`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.DisableKeyringFile = true
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-dns-port",
			args: []string{
				`-dns-port=123`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.DNSPort = 123
				rt.DNSAddrs = []net.Addr{tcpAddr("127.0.0.1:123"), udpAddr("127.0.0.1:123")}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-domain",
			args: []string{
				`-domain=a`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.DNSDomain = "a"
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-enable-script-checks",
			args: []string{
				`-enable-script-checks`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.EnableLocalScriptChecks = true
				rt.EnableRemoteScriptChecks = true
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-encrypt",
			args: []string{
				`-encrypt=i0P+gFTkLPg0h53eNYjydg==`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.EncryptKey = "i0P+gFTkLPg0h53eNYjydg=="
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-config-format disabled, skip unknown files",
			args: []string{
				`-data-dir=` + dataDir,
				`-config-dir`, filepath.Join(dataDir, "conf"),
			},
			patch: func(rt *RuntimeConfig) {
				rt.Datacenter = "a"
				rt.DataDir = dataDir
			},
			pre: func() {
				writeFile(filepath.Join(dataDir, "conf", "valid.json"), []byte(`{"datacenter":"a"}`))
				writeFile(filepath.Join(dataDir, "conf", "invalid.skip"), []byte(`NOPE`))
			},
		},
		{
			desc: "-config-format=json",
			args: []string{
				`-data-dir=` + dataDir,
				`-config-format=json`,
				`-config-file`, filepath.Join(dataDir, "conf"),
			},
			patch: func(rt *RuntimeConfig) {
				rt.Datacenter = "a"
				rt.DataDir = dataDir
			},
			pre: func() {
				writeFile(filepath.Join(dataDir, "conf"), []byte(`{"datacenter":"a"}`))
			},
		},
		{
			desc: "-config-format=hcl",
			args: []string{
				`-data-dir=` + dataDir,
				`-config-format=hcl`,
				`-config-file`, filepath.Join(dataDir, "conf"),
			},
			patch: func(rt *RuntimeConfig) {
				rt.Datacenter = "a"
				rt.DataDir = dataDir
			},
			pre: func() {
				writeFile(filepath.Join(dataDir, "conf"), []byte(`datacenter = "a"`))
			},
		},
		{
			desc: "-config-format invalid",
			args: []string{
				`-config-format=foobar`,
			},
			err: "-config-format must be either 'hcl' or 'json'",
		},
		{
			desc: "-http-port",
			args: []string{
				`-http-port=123`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.HTTPPort = 123
				rt.HTTPAddrs = []net.Addr{tcpAddr("127.0.0.1:123")}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-join",
			args: []string{
				`-join=a`,
				`-join=b`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.StartJoinAddrsLAN = []string{"a", "b"}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-join-wan",
			args: []string{
				`-join-wan=a`,
				`-join-wan=b`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.StartJoinAddrsWAN = []string{"a", "b"}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-log-level",
			args: []string{
				`-log-level=a`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.LogLevel = "a"
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-node",
			args: []string{
				`-node=a`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.NodeName = "a"
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-node-id",
			args: []string{
				`-node-id=a`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.NodeID = "a"
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-node-meta",
			args: []string{
				`-node-meta=a:b`,
				`-node-meta=c:d`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.NodeMeta = map[string]string{"a": "b", "c": "d"}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-non-voting-server",
			args: []string{
				`-non-voting-server`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.NonVotingServer = true
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-pid-file",
			args: []string{
				`-pid-file=a`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.PidFile = "a"
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-protocol",
			args: []string{
				`-protocol=1`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.RPCProtocol = 1
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-raft-protocol",
			args: []string{
				`-raft-protocol=1`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.RaftProtocol = 1
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-recursor",
			args: []string{
				`-recursor=1.2.3.4`,
				`-recursor=5.6.7.8`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.DNSRecursors = []string{"1.2.3.4", "5.6.7.8"}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-rejoin",
			args: []string{
				`-rejoin`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.RejoinAfterLeave = true
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-retry-interval",
			args: []string{
				`-retry-interval=5s`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.RetryJoinIntervalLAN = 5 * time.Second
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-retry-interval-wan",
			args: []string{
				`-retry-interval-wan=5s`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.RetryJoinIntervalWAN = 5 * time.Second
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-retry-join",
			args: []string{
				`-retry-join=a`,
				`-retry-join=b`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.RetryJoinLAN = []string{"a", "b"}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-retry-join-wan",
			args: []string{
				`-retry-join-wan=a`,
				`-retry-join-wan=b`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.RetryJoinWAN = []string{"a", "b"}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-retry-max",
			args: []string{
				`-retry-max=1`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.RetryJoinMaxAttemptsLAN = 1
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-retry-max-wan",
			args: []string{
				`-retry-max-wan=1`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.RetryJoinMaxAttemptsWAN = 1
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-serf-lan-bind",
			args: []string{
				`-serf-lan-bind=1.2.3.4`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.SerfBindAddrLAN = tcpAddr("1.2.3.4:8301")
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-serf-lan-port",
			args: []string{
				`-serf-lan-port=123`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.SerfPortLAN = 123
				rt.SerfAdvertiseAddrLAN = tcpAddr("10.0.0.1:123")
				rt.SerfBindAddrLAN = tcpAddr("0.0.0.0:123")
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-serf-wan-bind",
			args: []string{
				`-serf-wan-bind=1.2.3.4`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.SerfBindAddrWAN = tcpAddr("1.2.3.4:8302")
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-serf-wan-port",
			args: []string{
				`-serf-wan-port=123`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.SerfPortWAN = 123
				rt.SerfAdvertiseAddrWAN = tcpAddr("10.0.0.1:123")
				rt.SerfBindAddrWAN = tcpAddr("0.0.0.0:123")
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-server",
			args: []string{
				`-server`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.ServerMode = true
				rt.LeaveOnTerm = false
				rt.SkipLeaveOnInt = true
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-server-port",
			args: []string{
				`-server-port=123`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.ServerPort = 123
				rt.RPCAdvertiseAddr = tcpAddr("10.0.0.1:123")
				rt.RPCBindAddr = tcpAddr("0.0.0.0:123")
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-syslog",
			args: []string{
				`-syslog`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.EnableSyslog = true
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-ui",
			args: []string{
				`-ui`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.EnableUI = true
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-ui-dir",
			args: []string{
				`-ui-dir=a`,
				`-data-dir=` + dataDir,
			},
			patch: func(rt *RuntimeConfig) {
				rt.UIDir = "a"
				rt.DataDir = dataDir
			},
		},
		{
			desc: "-ui-content-path",
			args: []string{
				`-ui-content-path=/a/b`,
				`-data-dir=` + dataDir,
			},

			patch: func(rt *RuntimeConfig) {
				rt.UIContentPath = "/a/b/"
				rt.DataDir = dataDir
			},
		},

		// ------------------------------------------------------------
		// ports and addresses
		//

		{
			desc: "bind addr any v4",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "bind_addr":"0.0.0.0" }`},
			hcl:  []string{`bind_addr = "0.0.0.0"`},
			patch: func(rt *RuntimeConfig) {
				rt.AdvertiseAddrLAN = ipAddr("10.0.0.1")
				rt.AdvertiseAddrWAN = ipAddr("10.0.0.1")
				rt.BindAddr = ipAddr("0.0.0.0")
				rt.RPCAdvertiseAddr = tcpAddr("10.0.0.1:8300")
				rt.RPCBindAddr = tcpAddr("0.0.0.0:8300")
				rt.SerfAdvertiseAddrLAN = tcpAddr("10.0.0.1:8301")
				rt.SerfAdvertiseAddrWAN = tcpAddr("10.0.0.1:8302")
				rt.SerfBindAddrLAN = tcpAddr("0.0.0.0:8301")
				rt.SerfBindAddrWAN = tcpAddr("0.0.0.0:8302")
				rt.TaggedAddresses = map[string]string{
					"lan": "10.0.0.1",
					"wan": "10.0.0.1",
				}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "bind addr any v6",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "bind_addr":"::" }`},
			hcl:  []string{`bind_addr = "::"`},
			patch: func(rt *RuntimeConfig) {
				rt.AdvertiseAddrLAN = ipAddr("dead:beef::1")
				rt.AdvertiseAddrWAN = ipAddr("dead:beef::1")
				rt.BindAddr = ipAddr("::")
				rt.RPCAdvertiseAddr = tcpAddr("[dead:beef::1]:8300")
				rt.RPCBindAddr = tcpAddr("[::]:8300")
				rt.SerfAdvertiseAddrLAN = tcpAddr("[dead:beef::1]:8301")
				rt.SerfAdvertiseAddrWAN = tcpAddr("[dead:beef::1]:8302")
				rt.SerfBindAddrLAN = tcpAddr("[::]:8301")
				rt.SerfBindAddrWAN = tcpAddr("[::]:8302")
				rt.TaggedAddresses = map[string]string{
					"lan": "dead:beef::1",
					"wan": "dead:beef::1",
				}
				rt.DataDir = dataDir
			},
			publicv6: func() ([]*net.IPAddr, error) {
				return []*net.IPAddr{ipAddr("dead:beef::1")}, nil
			},
		},
		{
			desc: "bind addr any and advertise set should not detect",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "bind_addr":"0.0.0.0", "advertise_addr": "1.2.3.4" }`},
			hcl:  []string{`bind_addr = "0.0.0.0" advertise_addr = "1.2.3.4"`},
			patch: func(rt *RuntimeConfig) {
				rt.AdvertiseAddrLAN = ipAddr("1.2.3.4")
				rt.AdvertiseAddrWAN = ipAddr("1.2.3.4")
				rt.BindAddr = ipAddr("0.0.0.0")
				rt.RPCAdvertiseAddr = tcpAddr("1.2.3.4:8300")
				rt.RPCBindAddr = tcpAddr("0.0.0.0:8300")
				rt.SerfAdvertiseAddrLAN = tcpAddr("1.2.3.4:8301")
				rt.SerfAdvertiseAddrWAN = tcpAddr("1.2.3.4:8302")
				rt.SerfBindAddrLAN = tcpAddr("0.0.0.0:8301")
				rt.SerfBindAddrWAN = tcpAddr("0.0.0.0:8302")
				rt.TaggedAddresses = map[string]string{
					"lan": "1.2.3.4",
					"wan": "1.2.3.4",
				}
				rt.DataDir = dataDir
			},
			privatev4: func() ([]*net.IPAddr, error) {
				return nil, fmt.Errorf("should not detect advertise_addr")
			},
		},
		{
			desc: "client addr and ports == 0",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{
					"client_addr":"0.0.0.0",
					"ports":{}
				}`},
			hcl: []string{`
					client_addr = "0.0.0.0"
					ports {}
				`},
			patch: func(rt *RuntimeConfig) {
				rt.ClientAddrs = []*net.IPAddr{ipAddr("0.0.0.0")}
				rt.DNSAddrs = []net.Addr{tcpAddr("0.0.0.0:8600"), udpAddr("0.0.0.0:8600")}
				rt.HTTPAddrs = []net.Addr{tcpAddr("0.0.0.0:8500")}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "client addr and ports < 0",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{
					"client_addr":"0.0.0.0",
					"ports": { "dns":-1, "http":-2, "https":-3, "grpc":-4 }
				}`},
			hcl: []string{`
					client_addr = "0.0.0.0"
					ports { dns = -1 http = -2 https = -3 grpc = -4 }
				`},
			patch: func(rt *RuntimeConfig) {
				rt.ClientAddrs = []*net.IPAddr{ipAddr("0.0.0.0")}
				rt.DNSPort = -1
				rt.DNSAddrs = nil
				rt.HTTPPort = -1
				rt.HTTPAddrs = nil
				// HTTPS and gRPC default to disabled so shouldn't be different from
				// default rt.
				rt.DataDir = dataDir
			},
		},
		{
			desc: "client addr and ports > 0",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{
					"client_addr":"0.0.0.0",
					"ports":{ "dns": 1, "http": 2, "https": 3, "grpc": 4 }
				}`},
			hcl: []string{`
					client_addr = "0.0.0.0"
					ports { dns = 1 http = 2 https = 3 grpc = 4 }
				`},
			patch: func(rt *RuntimeConfig) {
				rt.ClientAddrs = []*net.IPAddr{ipAddr("0.0.0.0")}
				rt.DNSPort = 1
				rt.DNSAddrs = []net.Addr{tcpAddr("0.0.0.0:1"), udpAddr("0.0.0.0:1")}
				rt.HTTPPort = 2
				rt.HTTPAddrs = []net.Addr{tcpAddr("0.0.0.0:2")}
				rt.HTTPSPort = 3
				rt.HTTPSAddrs = []net.Addr{tcpAddr("0.0.0.0:3")}
				rt.GRPCPort = 4
				rt.GRPCAddrs = []net.Addr{tcpAddr("0.0.0.0:4")}
				rt.DataDir = dataDir
			},
		},

		{
			desc: "client addr, addresses and ports == 0",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{
					"client_addr":"0.0.0.0",
					"addresses": { "dns": "1.1.1.1", "http": "2.2.2.2", "https": "3.3.3.3", "grpc": "4.4.4.4" },
					"ports":{}
				}`},
			hcl: []string{`
					client_addr = "0.0.0.0"
					addresses = { dns = "1.1.1.1" http = "2.2.2.2" https = "3.3.3.3" grpc = "4.4.4.4" }
					ports {}
				`},
			patch: func(rt *RuntimeConfig) {
				rt.ClientAddrs = []*net.IPAddr{ipAddr("0.0.0.0")}
				rt.DNSAddrs = []net.Addr{tcpAddr("1.1.1.1:8600"), udpAddr("1.1.1.1:8600")}
				rt.HTTPAddrs = []net.Addr{tcpAddr("2.2.2.2:8500")}
				// HTTPS and gRPC default to disabled so shouldn't be different from
				// default rt.
				rt.DataDir = dataDir
			},
		},
		{
			desc: "client addr, addresses and ports < 0",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{
					"client_addr":"0.0.0.0",
					"addresses": { "dns": "1.1.1.1", "http": "2.2.2.2", "https": "3.3.3.3", "grpc": "4.4.4.4" },
					"ports": { "dns":-1, "http":-2, "https":-3, "grpc":-4 }
				}`},
			hcl: []string{`
					client_addr = "0.0.0.0"
					addresses = { dns = "1.1.1.1" http = "2.2.2.2" https = "3.3.3.3" grpc = "4.4.4.4" }
					ports { dns = -1 http = -2 https = -3 grpc = -4 }
				`},
			patch: func(rt *RuntimeConfig) {
				rt.ClientAddrs = []*net.IPAddr{ipAddr("0.0.0.0")}
				rt.DNSPort = -1
				rt.DNSAddrs = nil
				rt.HTTPPort = -1
				rt.HTTPAddrs = nil
				// HTTPS and gRPC default to disabled so shouldn't be different from
				// default rt.
				rt.DataDir = dataDir
			},
		},
		{
			desc: "client addr, addresses and ports",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{
					"client_addr": "0.0.0.0",
					"addresses": { "dns": "1.1.1.1", "http": "2.2.2.2", "https": "3.3.3.3", "grpc": "4.4.4.4" },
					"ports":{ "dns":1, "http":2, "https":3, "grpc":4 }
				}`},
			hcl: []string{`
					client_addr = "0.0.0.0"
					addresses = { dns = "1.1.1.1" http = "2.2.2.2" https = "3.3.3.3" grpc = "4.4.4.4" }
					ports { dns = 1 http = 2 https = 3 grpc = 4 }
				`},
			patch: func(rt *RuntimeConfig) {
				rt.ClientAddrs = []*net.IPAddr{ipAddr("0.0.0.0")}
				rt.DNSPort = 1
				rt.DNSAddrs = []net.Addr{tcpAddr("1.1.1.1:1"), udpAddr("1.1.1.1:1")}
				rt.HTTPPort = 2
				rt.HTTPAddrs = []net.Addr{tcpAddr("2.2.2.2:2")}
				rt.HTTPSPort = 3
				rt.HTTPSAddrs = []net.Addr{tcpAddr("3.3.3.3:3")}
				rt.GRPCPort = 4
				rt.GRPCAddrs = []net.Addr{tcpAddr("4.4.4.4:4")}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "client template and ports",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{
					"client_addr": "{{ printf \"1.2.3.4 2001:db8::1\" }}",
					"ports":{ "dns":1, "http":2, "https":3, "grpc":4 }
				}`},
			hcl: []string{`
					client_addr = "{{ printf \"1.2.3.4 2001:db8::1\" }}"
					ports { dns = 1 http = 2 https = 3 grpc = 4 }
				`},
			patch: func(rt *RuntimeConfig) {
				rt.ClientAddrs = []*net.IPAddr{ipAddr("1.2.3.4"), ipAddr("2001:db8::1")}
				rt.DNSPort = 1
				rt.DNSAddrs = []net.Addr{tcpAddr("1.2.3.4:1"), tcpAddr("[2001:db8::1]:1"), udpAddr("1.2.3.4:1"), udpAddr("[2001:db8::1]:1")}
				rt.HTTPPort = 2
				rt.HTTPAddrs = []net.Addr{tcpAddr("1.2.3.4:2"), tcpAddr("[2001:db8::1]:2")}
				rt.HTTPSPort = 3
				rt.HTTPSAddrs = []net.Addr{tcpAddr("1.2.3.4:3"), tcpAddr("[2001:db8::1]:3")}
				rt.GRPCPort = 4
				rt.GRPCAddrs = []net.Addr{tcpAddr("1.2.3.4:4"), tcpAddr("[2001:db8::1]:4")}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "client, address template and ports",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{
					"client_addr": "{{ printf \"1.2.3.4 2001:db8::1\" }}",
					"addresses": {
						"dns": "{{ printf \"1.1.1.1 2001:db8::10 \" }}",
						"http": "{{ printf \"2.2.2.2 unix://http 2001:db8::20 \" }}",
						"https": "{{ printf \"3.3.3.3 unix://https 2001:db8::30 \" }}",
						"grpc": "{{ printf \"4.4.4.4 unix://grpc 2001:db8::40 \" }}"
					},
					"ports":{ "dns":1, "http":2, "https":3, "grpc":4 }
				}`},
			hcl: []string{`
					client_addr = "{{ printf \"1.2.3.4 2001:db8::1\" }}"
					addresses = {
						dns = "{{ printf \"1.1.1.1 2001:db8::10 \" }}"
						http = "{{ printf \"2.2.2.2 unix://http 2001:db8::20 \" }}"
						https = "{{ printf \"3.3.3.3 unix://https 2001:db8::30 \" }}"
						grpc = "{{ printf \"4.4.4.4 unix://grpc 2001:db8::40 \" }}"
					}
					ports { dns = 1 http = 2 https = 3 grpc = 4 }
				`},
			patch: func(rt *RuntimeConfig) {
				rt.ClientAddrs = []*net.IPAddr{ipAddr("1.2.3.4"), ipAddr("2001:db8::1")}
				rt.DNSPort = 1
				rt.DNSAddrs = []net.Addr{tcpAddr("1.1.1.1:1"), tcpAddr("[2001:db8::10]:1"), udpAddr("1.1.1.1:1"), udpAddr("[2001:db8::10]:1")}
				rt.HTTPPort = 2
				rt.HTTPAddrs = []net.Addr{tcpAddr("2.2.2.2:2"), unixAddr("unix://http"), tcpAddr("[2001:db8::20]:2")}
				rt.HTTPSPort = 3
				rt.HTTPSAddrs = []net.Addr{tcpAddr("3.3.3.3:3"), unixAddr("unix://https"), tcpAddr("[2001:db8::30]:3")}
				rt.GRPCPort = 4
				rt.GRPCAddrs = []net.Addr{tcpAddr("4.4.4.4:4"), unixAddr("unix://grpc"), tcpAddr("[2001:db8::40]:4")}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "advertise address lan template",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "advertise_addr": "{{ printf \"1.2.3.4\" }}" }`},
			hcl:  []string{`advertise_addr = "{{ printf \"1.2.3.4\" }}"`},
			patch: func(rt *RuntimeConfig) {
				rt.AdvertiseAddrLAN = ipAddr("1.2.3.4")
				rt.AdvertiseAddrWAN = ipAddr("1.2.3.4")
				rt.RPCAdvertiseAddr = tcpAddr("1.2.3.4:8300")
				rt.SerfAdvertiseAddrLAN = tcpAddr("1.2.3.4:8301")
				rt.SerfAdvertiseAddrWAN = tcpAddr("1.2.3.4:8302")
				rt.TaggedAddresses = map[string]string{
					"lan": "1.2.3.4",
					"wan": "1.2.3.4",
				}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "advertise address wan template",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "advertise_addr_wan": "{{ printf \"1.2.3.4\" }}" }`},
			hcl:  []string{`advertise_addr_wan = "{{ printf \"1.2.3.4\" }}"`},
			patch: func(rt *RuntimeConfig) {
				rt.AdvertiseAddrWAN = ipAddr("1.2.3.4")
				rt.SerfAdvertiseAddrWAN = tcpAddr("1.2.3.4:8302")
				rt.TaggedAddresses = map[string]string{
					"lan": "10.0.0.1",
					"wan": "1.2.3.4",
				}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "advertise address lan with ports",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{
				"ports": {
					"server": 1000,
					"serf_lan": 2000,
					"serf_wan": 3000
				},
				"advertise_addr": "1.2.3.4"
			}`},
			hcl: []string{`
				ports {
					server = 1000
					serf_lan = 2000
					serf_wan = 3000
				}
				advertise_addr = "1.2.3.4"
			`},
			patch: func(rt *RuntimeConfig) {
				rt.AdvertiseAddrLAN = ipAddr("1.2.3.4")
				rt.AdvertiseAddrWAN = ipAddr("1.2.3.4")
				rt.RPCAdvertiseAddr = tcpAddr("1.2.3.4:1000")
				rt.RPCBindAddr = tcpAddr("0.0.0.0:1000")
				rt.SerfAdvertiseAddrLAN = tcpAddr("1.2.3.4:2000")
				rt.SerfAdvertiseAddrWAN = tcpAddr("1.2.3.4:3000")
				rt.SerfBindAddrLAN = tcpAddr("0.0.0.0:2000")
				rt.SerfBindAddrWAN = tcpAddr("0.0.0.0:3000")
				rt.SerfPortLAN = 2000
				rt.SerfPortWAN = 3000
				rt.ServerPort = 1000
				rt.TaggedAddresses = map[string]string{
					"lan": "1.2.3.4",
					"wan": "1.2.3.4",
				}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "advertise address wan with ports",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{
				"ports": {
					"server": 1000,
					"serf_lan": 2000,
					"serf_wan": 3000
				},
				"advertise_addr_wan": "1.2.3.4"
			}`},
			hcl: []string{`
				ports {
					server = 1000
					serf_lan = 2000
					serf_wan = 3000
				}
				advertise_addr_wan = "1.2.3.4"
			`},
			patch: func(rt *RuntimeConfig) {
				rt.AdvertiseAddrLAN = ipAddr("10.0.0.1")
				rt.AdvertiseAddrWAN = ipAddr("1.2.3.4")
				rt.RPCAdvertiseAddr = tcpAddr("10.0.0.1:1000")
				rt.RPCBindAddr = tcpAddr("0.0.0.0:1000")
				rt.SerfAdvertiseAddrLAN = tcpAddr("10.0.0.1:2000")
				rt.SerfAdvertiseAddrWAN = tcpAddr("1.2.3.4:3000")
				rt.SerfBindAddrLAN = tcpAddr("0.0.0.0:2000")
				rt.SerfBindAddrWAN = tcpAddr("0.0.0.0:3000")
				rt.SerfPortLAN = 2000
				rt.SerfPortWAN = 3000
				rt.ServerPort = 1000
				rt.TaggedAddresses = map[string]string{
					"lan": "10.0.0.1",
					"wan": "1.2.3.4",
				}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "allow disabling serf wan port",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{
				"ports": {
					"serf_wan": -1
				},
				"advertise_addr_wan": "1.2.3.4"
			}`},
			hcl: []string{`
				ports {
					serf_wan = -1
				}
				advertise_addr_wan = "1.2.3.4"
			`},
			patch: func(rt *RuntimeConfig) {
				rt.AdvertiseAddrWAN = ipAddr("1.2.3.4")
				rt.SerfAdvertiseAddrWAN = nil
				rt.SerfBindAddrWAN = nil
				rt.TaggedAddresses = map[string]string{
					"lan": "10.0.0.1",
					"wan": "1.2.3.4",
				}
				rt.DataDir = dataDir
				rt.SerfPortWAN = -1
			},
		},
		{
			desc: "serf bind address lan template",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "serf_lan": "{{ printf \"1.2.3.4\" }}" }`},
			hcl:  []string{`serf_lan = "{{ printf \"1.2.3.4\" }}"`},
			patch: func(rt *RuntimeConfig) {
				rt.SerfBindAddrLAN = tcpAddr("1.2.3.4:8301")
				rt.DataDir = dataDir
			},
		},
		{
			desc: "serf bind address wan template",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "serf_wan": "{{ printf \"1.2.3.4\" }}" }`},
			hcl:  []string{`serf_wan = "{{ printf \"1.2.3.4\" }}"`},
			patch: func(rt *RuntimeConfig) {
				rt.SerfBindAddrWAN = tcpAddr("1.2.3.4:8302")
				rt.DataDir = dataDir
			},
		},
		{
			desc: "dns recursor templates with deduplication",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "recursors": [ "{{ printf \"5.6.7.8:9999\" }}", "{{ printf \"1.2.3.4\" }}", "{{ printf \"5.6.7.8:9999\" }}" ] }`},
			hcl:  []string{`recursors = [ "{{ printf \"5.6.7.8:9999\" }}", "{{ printf \"1.2.3.4\" }}", "{{ printf \"5.6.7.8:9999\" }}" ] `},
			patch: func(rt *RuntimeConfig) {
				rt.DNSRecursors = []string{"5.6.7.8:9999", "1.2.3.4"}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "start_join address template",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "start_join": ["{{ printf \"1.2.3.4 4.3.2.1\" }}"] }`},
			hcl:  []string{`start_join = ["{{ printf \"1.2.3.4 4.3.2.1\" }}"]`},
			patch: func(rt *RuntimeConfig) {
				rt.StartJoinAddrsLAN = []string{"1.2.3.4", "4.3.2.1"}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "start_join_wan address template",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "start_join_wan": ["{{ printf \"1.2.3.4 4.3.2.1\" }}"] }`},
			hcl:  []string{`start_join_wan = ["{{ printf \"1.2.3.4 4.3.2.1\" }}"]`},
			patch: func(rt *RuntimeConfig) {
				rt.StartJoinAddrsWAN = []string{"1.2.3.4", "4.3.2.1"}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "retry_join address template",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "retry_join": ["{{ printf \"1.2.3.4 4.3.2.1\" }}"] }`},
			hcl:  []string{`retry_join = ["{{ printf \"1.2.3.4 4.3.2.1\" }}"]`},
			patch: func(rt *RuntimeConfig) {
				rt.RetryJoinLAN = []string{"1.2.3.4", "4.3.2.1"}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "retry_join_wan address template",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "retry_join_wan": ["{{ printf \"1.2.3.4 4.3.2.1\" }}"] }`},
			hcl:  []string{`retry_join_wan = ["{{ printf \"1.2.3.4 4.3.2.1\" }}"]`},
			patch: func(rt *RuntimeConfig) {
				rt.RetryJoinWAN = []string{"1.2.3.4", "4.3.2.1"}
				rt.DataDir = dataDir
			},
		},

		// ------------------------------------------------------------
		// precedence rules
		//

		{
			desc: "precedence: merge order",
			args: []string{`-data-dir=` + dataDir},
			json: []string{
				`{
						"bootstrap": true,
						"bootstrap_expect": 1,
						"datacenter": "a",
						"start_join": ["a", "b"],
						"node_meta": {"a":"b"}
					}`,
				`{
						"bootstrap": false,
						"bootstrap_expect": 0,
						"datacenter":"b",
						"start_join": ["c", "d"],
						"node_meta": {"a":"c"}
					}`,
			},
			hcl: []string{
				`
					bootstrap = true
					bootstrap_expect = 1
					datacenter = "a"
					start_join = ["a", "b"]
					node_meta = { "a" = "b" }
					`,
				`
					bootstrap = false
					bootstrap_expect = 0
					datacenter = "b"
					start_join = ["c", "d"]
					node_meta = { "a" = "c" }
					`,
			},
			patch: func(rt *RuntimeConfig) {
				rt.Bootstrap = false
				rt.BootstrapExpect = 0
				rt.Datacenter = "b"
				rt.StartJoinAddrsLAN = []string{"a", "b", "c", "d"}
				rt.NodeMeta = map[string]string{"a": "c"}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "precedence: flag before file",
			json: []string{
				`{
						"advertise_addr": "1.2.3.4",
						"advertise_addr_wan": "5.6.7.8",
						"bootstrap":true,
						"bootstrap_expect": 3,
						"datacenter":"a",
						"node_meta": {"a":"b"},
						"recursors":["1.2.3.5", "5.6.7.9"],
						"serf_lan": "a",
						"serf_wan": "a",
						"start_join":["a", "b"]
					}`,
			},
			hcl: []string{
				`
					advertise_addr = "1.2.3.4"
					advertise_addr_wan = "5.6.7.8"
					bootstrap = true
					bootstrap_expect = 3
					datacenter = "a"
					node_meta = { "a" = "b" }
					recursors = ["1.2.3.5", "5.6.7.9"]
					serf_lan = "a"
					serf_wan = "a"
					start_join = ["a", "b"]
					`,
			},
			args: []string{
				`-advertise=1.1.1.1`,
				`-advertise-wan=2.2.2.2`,
				`-bootstrap=false`,
				`-bootstrap-expect=0`,
				`-datacenter=b`,
				`-data-dir=` + dataDir,
				`-join`, `c`, `-join=d`,
				`-node-meta=a:c`,
				`-recursor`, `1.2.3.6`, `-recursor=5.6.7.10`,
				`-serf-lan-bind=3.3.3.3`,
				`-serf-wan-bind=4.4.4.4`,
			},
			patch: func(rt *RuntimeConfig) {
				rt.AdvertiseAddrLAN = ipAddr("1.1.1.1")
				rt.AdvertiseAddrWAN = ipAddr("2.2.2.2")
				rt.RPCAdvertiseAddr = tcpAddr("1.1.1.1:8300")
				rt.SerfAdvertiseAddrLAN = tcpAddr("1.1.1.1:8301")
				rt.SerfAdvertiseAddrWAN = tcpAddr("2.2.2.2:8302")
				rt.Datacenter = "b"
				rt.DNSRecursors = []string{"1.2.3.6", "5.6.7.10", "1.2.3.5", "5.6.7.9"}
				rt.NodeMeta = map[string]string{"a": "c"}
				rt.SerfBindAddrLAN = tcpAddr("3.3.3.3:8301")
				rt.SerfBindAddrWAN = tcpAddr("4.4.4.4:8302")
				rt.StartJoinAddrsLAN = []string{"c", "d", "a", "b"}
				rt.TaggedAddresses = map[string]string{
					"lan": "1.1.1.1",
					"wan": "2.2.2.2",
				}
				rt.DataDir = dataDir
			},
		},

		// ------------------------------------------------------------
		// transformations
		//

		{
			desc: "raft performance scaling",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "performance": { "raft_multiplier": 9} }`},
			hcl:  []string{`performance = { raft_multiplier=9 }`},
			patch: func(rt *RuntimeConfig) {
				rt.ConsulRaftElectionTimeout = 9 * 1000 * time.Millisecond
				rt.ConsulRaftHeartbeatTimeout = 9 * 1000 * time.Millisecond
				rt.ConsulRaftLeaderLeaseTimeout = 9 * 500 * time.Millisecond
				rt.DataDir = dataDir
			},
		},

		// ------------------------------------------------------------
		// validations
		//

		{
			desc: "invalid input",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`this is not JSON`},
			hcl:  []string{`*** 0123 this is not HCL`},
			err:  "Error parsing",
		},
		{
			desc: "datacenter is lower-cased",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "datacenter": "A" }`},
			hcl:  []string{`datacenter = "A"`},
			patch: func(rt *RuntimeConfig) {
				rt.Datacenter = "a"
				rt.DataDir = dataDir
			},
		},
		{
			desc: "acl_datacenter is lower-cased",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "acl_datacenter": "A" }`},
			hcl:  []string{`acl_datacenter = "A"`},
			patch: func(rt *RuntimeConfig) {
				rt.ACLsEnabled = true
				rt.ACLDatacenter = "a"
				rt.DataDir = dataDir
				rt.PrimaryDatacenter = "a"
			},
			warns: []string{`The 'acl_datacenter' field is deprecated. Use the 'primary_datacenter' field instead.`},
		},
		{
			desc: "acl_replication_token enables acl replication",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "acl_replication_token": "a" }`},
			hcl:  []string{`acl_replication_token = "a"`},
			patch: func(rt *RuntimeConfig) {
				rt.ACLReplicationToken = "a"
				rt.ACLTokenReplication = true
				rt.DataDir = dataDir
			},
		},
		{
			desc: "advertise address detect fails v4",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "bind_addr": "0.0.0.0"}`},
			hcl:  []string{`bind_addr = "0.0.0.0"`},
			privatev4: func() ([]*net.IPAddr, error) {
				return nil, errors.New("some error")
			},
			err: "Error detecting private IPv4 address: some error",
		},
		{
			desc: "advertise address detect none v4",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "bind_addr": "0.0.0.0"}`},
			hcl:  []string{`bind_addr = "0.0.0.0"`},
			privatev4: func() ([]*net.IPAddr, error) {
				return nil, nil
			},
			err: "No private IPv4 address found",
		},
		{
			desc: "advertise address detect multiple v4",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "bind_addr": "0.0.0.0"}`},
			hcl:  []string{`bind_addr = "0.0.0.0"`},
			privatev4: func() ([]*net.IPAddr, error) {
				return []*net.IPAddr{ipAddr("1.1.1.1"), ipAddr("2.2.2.2")}, nil
			},
			err: "Multiple private IPv4 addresses found. Please configure one",
		},
		{
			desc: "advertise address detect fails v6",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "bind_addr": "::"}`},
			hcl:  []string{`bind_addr = "::"`},
			publicv6: func() ([]*net.IPAddr, error) {
				return nil, errors.New("some error")
			},
			err: "Error detecting public IPv6 address: some error",
		},
		{
			desc: "advertise address detect none v6",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "bind_addr": "::"}`},
			hcl:  []string{`bind_addr = "::"`},
			publicv6: func() ([]*net.IPAddr, error) {
				return nil, nil
			},
			err: "No public IPv6 address found",
		},
		{
			desc: "advertise address detect multiple v6",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "bind_addr": "::"}`},
			hcl:  []string{`bind_addr = "::"`},
			publicv6: func() ([]*net.IPAddr, error) {
				return []*net.IPAddr{ipAddr("dead:beef::1"), ipAddr("dead:beef::2")}, nil
			},
			err: "Multiple public IPv6 addresses found. Please configure one",
		},
		{
			desc:     "ae_interval invalid == 0",
			args:     []string{`-data-dir=` + dataDir},
			jsontail: []string{`{ "ae_interval": "0s" }`},
			hcltail:  []string{`ae_interval = "0s"`},
			err:      `ae_interval cannot be 0s. Must be positive`,
		},
		{
			desc:     "ae_interval invalid < 0",
			args:     []string{`-data-dir=` + dataDir},
			jsontail: []string{`{ "ae_interval": "-1s" }`},
			hcltail:  []string{`ae_interval = "-1s"`},
			err:      `ae_interval cannot be -1s. Must be positive`,
		},
		{
			desc: "acl_datacenter invalid",
			args: []string{
				`-datacenter=a`,
				`-data-dir=` + dataDir,
			},
			json:  []string{`{ "acl_datacenter": "%" }`},
			hcl:   []string{`acl_datacenter = "%"`},
			err:   `acl_datacenter cannot be "%". Please use only [a-z0-9-_]`,
			warns: []string{`The 'acl_datacenter' field is deprecated. Use the 'primary_datacenter' field instead.`},
		},
		{
			desc: "autopilot.max_trailing_logs invalid",
			args: []string{
				`-datacenter=a`,
				`-data-dir=` + dataDir,
			},
			json: []string{`{ "autopilot": { "max_trailing_logs": -1 } }`},
			hcl:  []string{`autopilot = { max_trailing_logs = -1 }`},
			err:  "autopilot.max_trailing_logs cannot be -1. Must be greater than or equal to zero",
		},
		{
			desc: "bind_addr cannot be empty",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "bind_addr": "" }`},
			hcl:  []string{`bind_addr = ""`},
			err:  "bind_addr cannot be empty",
		},
		{
			desc: "bind_addr does not allow multiple addresses",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "bind_addr": "1.1.1.1 2.2.2.2" }`},
			hcl:  []string{`bind_addr = "1.1.1.1 2.2.2.2"`},
			err:  "bind_addr cannot contain multiple addresses",
		},
		{
			desc: "bind_addr cannot be a unix socket",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "bind_addr": "unix:///foo" }`},
			hcl:  []string{`bind_addr = "unix:///foo"`},
			err:  "bind_addr cannot be a unix socket",
		},
		{
			desc: "bootstrap without server",
			args: []string{
				`-datacenter=a`,
				`-data-dir=` + dataDir,
			},
			json: []string{`{ "bootstrap": true }`},
			hcl:  []string{`bootstrap = true`},
			err:  "'bootstrap = true' requires 'server = true'",
		},
		{
			desc: "bootstrap-expect without server",
			args: []string{
				`-datacenter=a`,
				`-data-dir=` + dataDir,
			},
			json: []string{`{ "bootstrap_expect": 3 }`},
			hcl:  []string{`bootstrap_expect = 3`},
			err:  "'bootstrap_expect > 0' requires 'server = true'",
		},
		{
			desc: "bootstrap-expect invalid",
			args: []string{
				`-datacenter=a`,
				`-data-dir=` + dataDir,
			},
			json: []string{`{ "bootstrap_expect": -1 }`},
			hcl:  []string{`bootstrap_expect = -1`},
			err:  "bootstrap_expect cannot be -1. Must be greater than or equal to zero",
		},
		{
			desc: "bootstrap-expect and dev mode",
			args: []string{
				`-dev`,
				`-datacenter=a`,
				`-data-dir=` + dataDir,
			},
			json: []string{`{ "bootstrap_expect": 3, "server": true }`},
			hcl:  []string{`bootstrap_expect = 3 server = true`},
			err:  "'bootstrap_expect > 0' not allowed in dev mode",
		},
		{
			desc: "bootstrap-expect and bootstrap",
			args: []string{
				`-datacenter=a`,
				`-data-dir=` + dataDir,
			},
			json: []string{`{ "bootstrap": true, "bootstrap_expect": 3, "server": true }`},
			hcl:  []string{`bootstrap = true bootstrap_expect = 3 server = true`},
			err:  "'bootstrap_expect > 0' and 'bootstrap = true' are mutually exclusive",
		},
		{
			desc: "bootstrap-expect=1 equals bootstrap",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{ "bootstrap_expect": 1, "server": true }`},
			hcl:  []string{`bootstrap_expect = 1 server = true`},
			patch: func(rt *RuntimeConfig) {
				rt.Bootstrap = true
				rt.BootstrapExpect = 0
				rt.LeaveOnTerm = false
				rt.ServerMode = true
				rt.SkipLeaveOnInt = true
				rt.DataDir = dataDir
			},
			warns: []string{"BootstrapExpect is set to 1; this is the same as Bootstrap mode.", "bootstrap = true: do not enable unless necessary"},
		},
		{
			desc: "bootstrap-expect=2 warning",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{ "bootstrap_expect": 2, "server": true }`},
			hcl:  []string{`bootstrap_expect = 2 server = true`},
			patch: func(rt *RuntimeConfig) {
				rt.BootstrapExpect = 2
				rt.LeaveOnTerm = false
				rt.ServerMode = true
				rt.SkipLeaveOnInt = true
				rt.DataDir = dataDir
			},
			warns: []string{
				`bootstrap_expect = 2: A cluster with 2 servers will provide no failure tolerance. See https://www.consul.io/docs/internals/consensus.html#deployment-table`,
				`bootstrap_expect > 0: expecting 2 servers`,
			},
		},
		{
			desc: "bootstrap-expect > 2 but even warning",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{ "bootstrap_expect": 4, "server": true }`},
			hcl:  []string{`bootstrap_expect = 4 server = true`},
			patch: func(rt *RuntimeConfig) {
				rt.BootstrapExpect = 4
				rt.LeaveOnTerm = false
				rt.ServerMode = true
				rt.SkipLeaveOnInt = true
				rt.DataDir = dataDir
			},
			warns: []string{
				`bootstrap_expect is even number: A cluster with an even number of servers does not achieve optimum fault tolerance. See https://www.consul.io/docs/internals/consensus.html#deployment-table`,
				`bootstrap_expect > 0: expecting 4 servers`,
			},
		},
		{
			desc: "client mode sets LeaveOnTerm and SkipLeaveOnInt correctly",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{ "server": false }`},
			hcl:  []string{` server = false`},
			patch: func(rt *RuntimeConfig) {
				rt.LeaveOnTerm = true
				rt.ServerMode = false
				rt.SkipLeaveOnInt = false
				rt.DataDir = dataDir
			},
		},
		{
			desc: "client does not allow socket",
			args: []string{
				`-datacenter=a`,
				`-data-dir=` + dataDir,
			},
			json: []string{`{ "client_addr": "unix:///foo" }`},
			hcl:  []string{`client_addr = "unix:///foo"`},
			err:  "client_addr cannot be a unix socket",
		},
		{
			desc: "datacenter invalid",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{ "datacenter": "%" }`},
			hcl:  []string{`datacenter = "%"`},
			err:  `datacenter cannot be "%". Please use only [a-z0-9-_]`,
		},
		{
			desc: "dns does not allow socket",
			args: []string{
				`-datacenter=a`,
				`-data-dir=` + dataDir,
			},
			json: []string{`{ "addresses": {"dns": "unix:///foo" } }`},
			hcl:  []string{`addresses = { dns = "unix:///foo" }`},
			err:  "DNS address cannot be a unix socket",
		},
		{
			desc: "ui and ui_dir",
			args: []string{
				`-datacenter=a`,
				`-data-dir=` + dataDir,
			},
			json: []string{`{ "ui": true, "ui_dir": "a" }`},
			hcl:  []string{`ui = true ui_dir = "a"`},
			err: "Both the ui and ui-dir flags were specified, please provide only one.\n" +
				"If trying to use your own web UI resources, use the ui-dir flag.\n" +
				"If using Consul version 0.7.0 or later, the web UI is included in the binary so use ui to enable it",
		},

		// test ANY address failures
		// to avoid combinatory explosion for tests use 0.0.0.0, :: or [::] but not all of them
		{
			desc: "advertise_addr any",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{ "advertise_addr": "0.0.0.0" }`},
			hcl:  []string{`advertise_addr = "0.0.0.0"`},
			err:  "Advertise address cannot be 0.0.0.0, :: or [::]",
		},
		{
			desc: "advertise_addr_wan any",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{ "advertise_addr_wan": "::" }`},
			hcl:  []string{`advertise_addr_wan = "::"`},
			err:  "Advertise WAN address cannot be 0.0.0.0, :: or [::]",
		},
		{
			desc: "recursors any",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{ "recursors": ["::"] }`},
			hcl:  []string{`recursors = ["::"]`},
			err:  "DNS recursor address cannot be 0.0.0.0, :: or [::]",
		},
		{
			desc: "dns_config.udp_answer_limit invalid",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{ "dns_config": { "udp_answer_limit": -1 } }`},
			hcl:  []string{`dns_config = { udp_answer_limit = -1 }`},
			err:  "dns_config.udp_answer_limit cannot be -1. Must be greater than or equal to zero",
		},
		{
			desc: "dns_config.a_record_limit invalid",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{ "dns_config": { "a_record_limit": -1 } }`},
			hcl:  []string{`dns_config = { a_record_limit = -1 }`},
			err:  "dns_config.a_record_limit cannot be -1. Must be greater than or equal to zero",
		},
		{
			desc: "performance.raft_multiplier < 0",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{ "performance": { "raft_multiplier": -1 } }`},
			hcl:  []string{`performance = { raft_multiplier = -1 }`},
			err:  `performance.raft_multiplier cannot be -1. Must be between 1 and 10`,
		},
		{
			desc: "performance.raft_multiplier == 0",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{ "performance": { "raft_multiplier": 0 } }`},
			hcl:  []string{`performance = { raft_multiplier = 0 }`},
			err:  `performance.raft_multiplier cannot be 0. Must be between 1 and 10`,
		},
		{
			desc: "performance.raft_multiplier > 10",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{ "performance": { "raft_multiplier": 20 } }`},
			hcl:  []string{`performance = { raft_multiplier = 20 }`},
			err:  `performance.raft_multiplier cannot be 20. Must be between 1 and 10`,
		},
		{
			desc: "node_name invalid",
			args: []string{
				`-data-dir=` + dataDir,
				`-node=`,
			},
			hostname: func() (string, error) { return "", nil },
			err:      "node_name cannot be empty",
		},
		{
			desc: "node_meta key too long",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{
				`{ "dns_config": { "udp_answer_limit": 1 } }`,
				`{ "node_meta": { "` + randomString(130) + `": "a" } }`,
			},
			hcl: []string{
				`dns_config = { udp_answer_limit = 1 }`,
				`node_meta = { "` + randomString(130) + `" = "a" }`,
			},
			err: "Key is too long (limit: 128 characters)",
		},
		{
			desc: "node_meta value too long",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{
				`{ "dns_config": { "udp_answer_limit": 1 } }`,
				`{ "node_meta": { "a": "` + randomString(520) + `" } }`,
			},
			hcl: []string{
				`dns_config = { udp_answer_limit = 1 }`,
				`node_meta = { "a" = "` + randomString(520) + `" }`,
			},
			err: "Value is too long (limit: 512 characters)",
		},
		{
			desc: "node_meta too many keys",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{
				`{ "dns_config": { "udp_answer_limit": 1 } }`,
				`{ "node_meta": {` + metaPairs(70, "json") + `} }`,
			},
			hcl: []string{
				`dns_config = { udp_answer_limit = 1 }`,
				`node_meta = {` + metaPairs(70, "hcl") + ` }`,
			},
			err: "Node metadata cannot contain more than 64 key/value pairs",
		},
		{
			desc: "unique listeners dns vs http",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{
					"client_addr": "1.2.3.4",
					"ports": { "dns": 1000, "http": 1000 }
				}`},
			hcl: []string{`
					client_addr = "1.2.3.4"
					ports = { dns = 1000 http = 1000 }
				`},
			err: "HTTP address 1.2.3.4:1000 already configured for DNS",
		},
		{
			desc: "unique listeners dns vs https",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{
					"client_addr": "1.2.3.4",
					"ports": { "dns": 1000, "https": 1000 }
				}`},
			hcl: []string{`
					client_addr = "1.2.3.4"
					ports = { dns = 1000 https = 1000 }
				`},
			err: "HTTPS address 1.2.3.4:1000 already configured for DNS",
		},
		{
			desc: "unique listeners http vs https",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{
					"client_addr": "1.2.3.4",
					"ports": { "http": 1000, "https": 1000 }
				}`},
			hcl: []string{`
					client_addr = "1.2.3.4"
					ports = { http = 1000 https = 1000 }
				`},
			err: "HTTPS address 1.2.3.4:1000 already configured for HTTP",
		},
		{
			desc: "unique advertise addresses HTTP vs RPC",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{
					"addresses": { "http": "10.0.0.1" },
					"ports": { "http": 1000, "server": 1000 }
				}`},
			hcl: []string{`
					addresses = { http = "10.0.0.1" }
					ports = { http = 1000 server = 1000 }
				`},
			err: "RPC Advertise address 10.0.0.1:1000 already configured for HTTP",
		},
		{
			desc: "unique advertise addresses RPC vs Serf LAN",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{
					"ports": { "server": 1000, "serf_lan": 1000 }
				}`},
			hcl: []string{`
					ports = { server = 1000 serf_lan = 1000 }
				`},
			err: "Serf Advertise LAN address 10.0.0.1:1000 already configured for RPC Advertise",
		},
		{
			desc: "unique advertise addresses RPC vs Serf WAN",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{
					"ports": { "server": 1000, "serf_wan": 1000 }
				}`},
			hcl: []string{`
					ports = { server = 1000 serf_wan = 1000 }
				`},
			err: "Serf Advertise WAN address 10.0.0.1:1000 already configured for RPC Advertise",
		},
		{
			desc: "sidecar_service can't have ID",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{
				  "service": {
						"name": "web",
						"port": 1234,
						"connect": {
							"sidecar_service": {
								"ID": "random-sidecar-id"
							}
						}
					}
				}`},
			hcl: []string{`
				service {
					name = "web"
					port = 1234
					connect {
						sidecar_service {
							ID = "random-sidecar-id"
						}
					}
				}
			`},
			err: "sidecar_service can't specify an ID",
		},
		{
			desc: "sidecar_service can't have nested sidecar",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{
				  "service": {
						"name": "web",
						"port": 1234,
						"connect": {
							"sidecar_service": {
								"connect": {
									"sidecar_service": {}
								}
							}
						}
					}
				}`},
			hcl: []string{`
				service {
					name = "web"
					port = 1234
					connect {
						sidecar_service {
							connect {
								sidecar_service {
								}
							}
						}
					}
				}
			`},
			err: "sidecar_service can't have a nested sidecar_service",
		},
		{
			desc: "sidecar_service can't have managed proxy",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{
				  "service": {
						"name": "web",
						"port": 1234,
						"connect": {
							"sidecar_service": {
								"connect": {
									"proxy": {}
								}
							}
						}
					}
				}`},
			hcl: []string{`
				service {
					name = "web"
					port = 1234
					connect {
						sidecar_service {
							connect {
								proxy {
								}
							}
						}
					}
				}
			`},
			err: "sidecar_service can't have a managed proxy",
		},
		{
			desc: "telemetry.prefix_filter cannot be empty",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{
					"telemetry": { "prefix_filter": [""] }
				}`},
			hcl: []string{`
					telemetry = { prefix_filter = [""] }
				`},
			patch: func(rt *RuntimeConfig) {
				rt.DataDir = dataDir
			},
			warns: []string{"Cannot have empty filter rule in prefix_filter"},
		},
		{
			desc: "telemetry.prefix_filter must start with + or -",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{
					"telemetry": { "prefix_filter": ["+foo", "-bar", "nix"] }
				}`},
			hcl: []string{`
					telemetry = { prefix_filter = ["+foo", "-bar", "nix"] }
				`},
			patch: func(rt *RuntimeConfig) {
				rt.DataDir = dataDir
				rt.Telemetry.AllowedPrefixes = []string{"foo"}
				rt.Telemetry.BlockedPrefixes = []string{"bar"}
			},
			warns: []string{`Filter rule must begin with either '+' or '-': "nix"`},
		},
		{
			desc: "encrypt has invalid key",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{ "encrypt": "this is not a valid key" }`},
			hcl:  []string{` encrypt = "this is not a valid key" `},
			err:  "encrypt has invalid key: illegal base64 data at input byte 4",
		},
		{
			desc: "encrypt given but LAN keyring exists",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{ "encrypt": "i0P+gFTkLPg0h53eNYjydg==" }`},
			hcl:  []string{` encrypt = "i0P+gFTkLPg0h53eNYjydg==" `},
			patch: func(rt *RuntimeConfig) {
				rt.EncryptKey = "i0P+gFTkLPg0h53eNYjydg=="
				rt.DataDir = dataDir
			},
			pre: func() {
				writeFile(filepath.Join(dataDir, SerfLANKeyring), []byte("i0P+gFTkLPg0h53eNYjydg=="))
			},
			warns: []string{`WARNING: LAN keyring exists but -encrypt given, using keyring`},
		},
		{
			desc: "encrypt given but WAN keyring exists",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{ "encrypt": "i0P+gFTkLPg0h53eNYjydg==", "server": true }`},
			hcl:  []string{` encrypt = "i0P+gFTkLPg0h53eNYjydg==" server = true `},
			patch: func(rt *RuntimeConfig) {
				rt.EncryptKey = "i0P+gFTkLPg0h53eNYjydg=="
				rt.ServerMode = true
				rt.LeaveOnTerm = false
				rt.SkipLeaveOnInt = true
				rt.DataDir = dataDir
			},
			pre: func() {
				writeFile(filepath.Join(dataDir, SerfWANKeyring), []byte("i0P+gFTkLPg0h53eNYjydg=="))
			},
			warns: []string{`WARNING: WAN keyring exists but -encrypt given, using keyring`},
		},
		{
			desc: "multiple check files",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{
				`{ "check": { "name": "a", "args": ["/bin/true"] } }`,
				`{ "check": { "name": "b", "args": ["/bin/false"] } }`,
			},
			hcl: []string{
				`check = { name = "a" args = ["/bin/true"] }`,
				`check = { name = "b" args = ["/bin/false"] }`,
			},
			patch: func(rt *RuntimeConfig) {
				rt.Checks = []*structs.CheckDefinition{
					&structs.CheckDefinition{Name: "a", ScriptArgs: []string{"/bin/true"}, OutputMaxSize: checks.DefaultBufSize},
					&structs.CheckDefinition{Name: "b", ScriptArgs: []string{"/bin/false"}, OutputMaxSize: checks.DefaultBufSize},
				}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "grpc check",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{
				`{ "check": { "name": "a", "grpc": "localhost:12345/foo", "grpc_use_tls": true } }`,
			},
			hcl: []string{
				`check = { name = "a" grpc = "localhost:12345/foo", grpc_use_tls = true }`,
			},
			patch: func(rt *RuntimeConfig) {
				rt.Checks = []*structs.CheckDefinition{
					&structs.CheckDefinition{Name: "a", GRPC: "localhost:12345/foo", GRPCUseTLS: true, OutputMaxSize: checks.DefaultBufSize},
				}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "alias check with no node",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{
				`{ "check": { "name": "a", "alias_service": "foo" } }`,
			},
			hcl: []string{
				`check = { name = "a", alias_service = "foo" }`,
			},
			patch: func(rt *RuntimeConfig) {
				rt.Checks = []*structs.CheckDefinition{
					&structs.CheckDefinition{Name: "a", AliasService: "foo", OutputMaxSize: checks.DefaultBufSize},
				}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "multiple service files",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{
				`{ "service": { "name": "a", "port": 80 } }`,
				`{ "service": { "name": "b", "port": 90, "meta": {"my": "value"}, "weights": {"passing": 13} } }`,
			},
			hcl: []string{
				`service = { name = "a" port = 80 }`,
				`service = { name = "b" port = 90 meta={my="value"}, weights={passing=13}}`,
			},
			patch: func(rt *RuntimeConfig) {
				rt.Services = []*structs.ServiceDefinition{
					&structs.ServiceDefinition{Name: "a", Port: 80, Weights: &structs.Weights{
						Passing: 1,
						Warning: 1,
					}},
					&structs.ServiceDefinition{Name: "b", Port: 90, Meta: map[string]string{"my": "value"}, Weights: &structs.Weights{
						Passing: 13,
						Warning: 1,
					}},
				}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "service with wrong meta: too long key",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{
				`{ "service": { "name": "a", "port": 80, "meta": { "` + randomString(520) + `": "metaValue" } } }`,
			},
			hcl: []string{
				`service = { name = "a" port = 80, meta={` + randomString(520) + `="metaValue"} }`,
			},
			err: `Key is too long`,
		},
		{
			desc: "service with wrong meta: too long value",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{
				`{ "service": { "name": "a", "port": 80, "meta": { "a": "` + randomString(520) + `" } } }`,
			},
			hcl: []string{
				`service = { name = "a" port = 80, meta={a="` + randomString(520) + `"} }`,
			},
			err: `Value is too long`,
		},
		{
			desc: "service with wrong meta: too many meta",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{
				`{ "service": { "name": "a", "port": 80, "meta": { ` + metaPairs(70, "json") + `} } }`,
			},
			hcl: []string{
				`service = { name = "a" port = 80 meta={` + metaPairs(70, "hcl") + `} }`,
			},
			err: `invalid meta for service a: Node metadata cannot contain more than 64 key`,
		},
		{
			desc: "translated keys",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{
				`{
					"service": {
						"name": "a",
						"port": 80,
						"enable_tag_override": true,
						"check": {
							"id": "x",
							"name": "y",
							"DockerContainerID": "z",
							"DeregisterCriticalServiceAfter": "10s",
							"ScriptArgs": ["a", "b"]
						}
					}
				}`,
			},
			hcl: []string{
				`service = {
					name = "a"
					port = 80
					enable_tag_override = true
					check = {
						id = "x"
						name = "y"
						DockerContainerID = "z"
						DeregisterCriticalServiceAfter = "10s"
						ScriptArgs = ["a", "b"]
					}
				}`,
			},
			patch: func(rt *RuntimeConfig) {
				rt.Services = []*structs.ServiceDefinition{
					&structs.ServiceDefinition{
						Name:              "a",
						Port:              80,
						EnableTagOverride: true,
						Checks: []*structs.CheckType{
							&structs.CheckType{
								CheckID:                        types.CheckID("x"),
								Name:                           "y",
								DockerContainerID:              "z",
								DeregisterCriticalServiceAfter: 10 * time.Second,
								ScriptArgs:                     []string{"a", "b"},
								OutputMaxSize:                  checks.DefaultBufSize,
							},
						},
						Weights: &structs.Weights{
							Passing: 1,
							Warning: 1,
						},
					},
				}
				rt.DataDir = dataDir
			},
		},
		{
			desc: "ignore snapshot_agent sub-object",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{
				`{ "snapshot_agent": { "dont": "care" } }`,
			},
			hcl: []string{
				`snapshot_agent = { dont = "care" }`,
			},
			patch: func(rt *RuntimeConfig) {
				rt.DataDir = dataDir
			},
		},

		{
			desc: "Service managed proxy 'upstreams'",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{
				`{
						"service": {
							"name": "web",
							"port": 8080,
							"connect": {
								"proxy": {
									"upstreams": [{
										"destination_name": "db",
										"local_bind_port": 1234
									}]
								}
							}
						}
					}`,
			},
			hcl: []string{
				`service {
					name = "web"
					port = 8080
					connect {
						proxy {
							upstreams {
								destination_name = "db"
								local_bind_port = 1234
							}
						}
					}
				}`,
			},
			patch: func(rt *RuntimeConfig) {
				rt.DataDir = dataDir
				rt.Services = []*structs.ServiceDefinition{
					&structs.ServiceDefinition{
						Name: "web",
						Port: 8080,
						Connect: &structs.ServiceConnect{
							Proxy: &structs.ServiceDefinitionConnectProxy{
								Upstreams: structs.Upstreams{
									{
										DestinationName: "db",
										DestinationType: structs.UpstreamDestTypeService,
										LocalBindPort:   1234,
									},
								},
							},
						},
						Weights: &structs.Weights{
							Passing: 1,
							Warning: 1,
						},
					},
				}
			},
		},

		{
			desc: "Multiple service managed proxy 'upstreams'",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{
				`{
						"service": {
							"name": "web",
							"port": 8080,
							"connect": {
								"proxy": {
									"upstreams": [{
										"destination_name": "db",
										"local_bind_port": 1234
									}, {
										"destination_name": "cache",
										"local_bind_port": 2345
									}]
								}
							}
						}
					}`,
			},
			hcl: []string{
				`service {
					name = "web"
					port = 8080
					connect {
						proxy {
							upstreams = [
								{
									destination_name = "db"
									local_bind_port = 1234
								},
							  {
									destination_name = "cache"
									local_bind_port = 2345
								}
							]
						}
					}
				}`,
			},
			patch: func(rt *RuntimeConfig) {
				rt.DataDir = dataDir
				rt.Services = []*structs.ServiceDefinition{
					&structs.ServiceDefinition{
						Name: "web",
						Port: 8080,
						Connect: &structs.ServiceConnect{
							Proxy: &structs.ServiceDefinitionConnectProxy{
								Upstreams: structs.Upstreams{
									{
										DestinationName: "db",
										DestinationType: structs.UpstreamDestTypeService,
										LocalBindPort:   1234,
									},
									{
										DestinationName: "cache",
										DestinationType: structs.UpstreamDestTypeService,
										LocalBindPort:   2345,
									},
								},
							},
						},
						Weights: &structs.Weights{
							Passing: 1,
							Warning: 1,
						},
					},
				}
			},
		},

		{
			desc: "enabling Connect allow_managed_root",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{
				`{ "connect": { "proxy": { "allow_managed_root": true } } }`,
			},
			hcl: []string{
				`connect { proxy { allow_managed_root = true } }`,
			},
			patch: func(rt *RuntimeConfig) {
				rt.DataDir = dataDir
				rt.ConnectProxyAllowManagedRoot = true
			},
		},

		{
			desc: "enabling Connect allow_managed_api_registration",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{
				`{ "connect": { "proxy": { "allow_managed_api_registration": true } } }`,
			},
			hcl: []string{
				`connect { proxy { allow_managed_api_registration = true } }`,
			},
			patch: func(rt *RuntimeConfig) {
				rt.DataDir = dataDir
				rt.ConnectProxyAllowManagedAPIRegistration = true
			},
		},

		{
			// This tests that we correct added the nested paths to arrays of objects
			// to the exceptions in patchSliceOfMaps in config.go (for single service)
			desc: "service.connectsidecar_service with checks and upstreams",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{
				  "service": {
						"name": "web",
						"port": 1234,
						"connect": {
							"sidecar_service": {
								"port": 2345,
								"checks": [
									{
										"TCP": "127.0.0.1:2345",
										"Interval": "10s"
									}
								],
								"proxy": {
									"upstreams": [
										{
											"destination_name": "db",
											"local_bind_port": 7000
										}
									]
								}
							}
						}
					}
				}`},
			hcl: []string{`
				service {
					name = "web"
					port = 1234
					connect {
						sidecar_service {
							port = 2345
							checks = [
								{
									tcp = "127.0.0.1:2345"
									interval = "10s"
								}
							]
							proxy {
								upstreams = [
									{
										destination_name = "db"
										local_bind_port = 7000
									},
								]
							}
						}
					}
				}
			`},
			patch: func(rt *RuntimeConfig) {
				rt.DataDir = dataDir
				rt.Services = []*structs.ServiceDefinition{
					{
						Name: "web",
						Port: 1234,
						Connect: &structs.ServiceConnect{
							SidecarService: &structs.ServiceDefinition{
								Port: 2345,
								Checks: structs.CheckTypes{
									{
										TCP:           "127.0.0.1:2345",
										Interval:      10 * time.Second,
										OutputMaxSize: checks.DefaultBufSize,
									},
								},
								Proxy: &structs.ConnectProxyConfig{
									Upstreams: structs.Upstreams{
										structs.Upstream{
											DestinationType: "service",
											DestinationName: "db",
											LocalBindPort:   7000,
										},
									},
								},
								Weights: &structs.Weights{
									Passing: 1,
									Warning: 1,
								},
							},
						},
						Weights: &structs.Weights{
							Passing: 1,
							Warning: 1,
						},
					},
				}
			},
		},
		{
			// This tests that we correct added the nested paths to arrays of objects
			// to the exceptions in patchSliceOfMaps in config.go (for service*s*)
			desc: "services.connect.sidecar_service with checks and upstreams",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{
				  "services": [{
						"name": "web",
						"port": 1234,
						"connect": {
							"sidecar_service": {
								"port": 2345,
								"checks": [
									{
										"TCP": "127.0.0.1:2345",
										"Interval": "10s"
									}
								],
								"proxy": {
									"upstreams": [
										{
											"destination_name": "db",
											"local_bind_port": 7000
										}
									]
								}
							}
						}
					}]
				}`},
			hcl: []string{`
				services = [{
					name = "web"
					port = 1234
					connect {
						sidecar_service {
							port = 2345
							checks = [
								{
									tcp = "127.0.0.1:2345"
									interval = "10s"
								}
							]
							proxy {
								upstreams = [
									{
										destination_name = "db"
										local_bind_port = 7000
									},
								]
							}
						}
					}
				}]
			`},
			patch: func(rt *RuntimeConfig) {
				rt.DataDir = dataDir
				rt.Services = []*structs.ServiceDefinition{
					{
						Name: "web",
						Port: 1234,
						Connect: &structs.ServiceConnect{
							SidecarService: &structs.ServiceDefinition{
								Port: 2345,
								Checks: structs.CheckTypes{
									{
										TCP:           "127.0.0.1:2345",
										Interval:      10 * time.Second,
										OutputMaxSize: checks.DefaultBufSize,
									},
								},
								Proxy: &structs.ConnectProxyConfig{
									Upstreams: structs.Upstreams{
										structs.Upstream{
											DestinationType: "service",
											DestinationName: "db",
											LocalBindPort:   7000,
										},
									},
								},
								Weights: &structs.Weights{
									Passing: 1,
									Warning: 1,
								},
							},
						},
						Weights: &structs.Weights{
							Passing: 1,
							Warning: 1,
						},
					},
				}
			},
		},
		{
			// This tests checks that VerifyServerHostname implies VerifyOutgoing
			desc: "verify_server_hostname implies verify_outgoing",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{
			  "verify_server_hostname": true
			}`},
			hcl: []string{`
			  verify_server_hostname = true
			`},
			patch: func(rt *RuntimeConfig) {
				rt.DataDir = dataDir
				rt.VerifyServerHostname = true
				rt.VerifyOutgoing = true
			},
		},
		{
			desc: "test connect vault provider configuration",
			args: []string{
				`-data-dir=` + dataDir,
			},
			json: []string{`{
				"connect": {
					"enabled": true,
					"ca_provider": "vault",
					"ca_config": {
						"ca_file": "/capath/ca.pem",
						"ca_path": "/capath/",
						"cert_file": "/certpath/cert.pem",
						"key_file": "/certpath/key.pem",
						"tls_server_name": "server.name",
						"tls_skip_verify": true,
						"token": "abc",
						"root_pki_path": "consul-vault",
						"intermediate_pki_path": "connect-intermediate"
					}
				}
			}`},
			hcl: []string{`
			  connect {
					enabled = true
					ca_provider = "vault"
					ca_config {
						ca_file = "/capath/ca.pem"
						ca_path = "/capath/"
						cert_file = "/certpath/cert.pem"
						key_file = "/certpath/key.pem"
						tls_server_name = "server.name"
						tls_skip_verify = true
						token = "abc"
						root_pki_path = "consul-vault"
						intermediate_pki_path = "connect-intermediate"
					}
				}
			`},
			patch: func(rt *RuntimeConfig) {
				rt.DataDir = dataDir
				rt.ConnectEnabled = true
				rt.ConnectCAProvider = "vault"
				rt.ConnectCAConfig = map[string]interface{}{
					"CAFile":              "/capath/ca.pem",
					"CAPath":              "/capath/",
					"CertFile":            "/certpath/cert.pem",
					"KeyFile":             "/certpath/key.pem",
					"TLSServerName":       "server.name",
					"TLSSkipVerify":       true,
					"Token":               "abc",
					"RootPKIPath":         "consul-vault",
					"IntermediatePKIPath": "connect-intermediate",
				}
			},
		},

		// ------------------------------------------------------------
		// ConfigEntry Handling
		//
		{
			desc: "ConfigEntry bootstrap doesn't parse",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{
				"config_entries": {
					"bootstrap": [
						{
							"foo": "bar"
						}
					]
				}
			}`},
			hcl: []string{`
			config_entries {
				bootstrap {
					foo = "bar"
				}
			}`},
			err: "config_entries.bootstrap[0]: Payload does not contain a Kind",
		},
		{
			desc: "ConfigEntry bootstrap unknown kind",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{
				"config_entries": {
					"bootstrap": [
						{
							"kind": "foo",
							"name": "bar",
							"baz": 1
						}
					]
				}
			}`},
			hcl: []string{`
			config_entries {
				bootstrap {
					kind = "foo"
					name = "bar"
					baz = 1
				}
			}`},
			err: "config_entries.bootstrap[0]: invalid config entry kind: foo",
		},
		{
			desc: "ConfigEntry bootstrap invalid",
			args: []string{`-data-dir=` + dataDir},
			json: []string{`{
				"config_entries": {
					"bootstrap": [
						{
							"kind": "proxy-defaults",
							"name": "invalid-name",
							"config": {
								"foo": "bar"
							}
						}
					]
				}
			}`},
			hcl: []string{`
			config_entries {
				bootstrap {
					kind = "proxy-defaults"
					name = "invalid-name"
					config {
						foo = "bar"
					}
				}
			}`},
			err: "config_entries.bootstrap[0]: invalid name (\"invalid-name\"), only \"global\" is supported",
		},
	}

	testConfig(t, tests, dataDir)
}

func testConfig(t *testing.T, tests []configTest, dataDir string) {
	for _, tt := range tests {
		for pass, format := range []string{"json", "hcl"} {
			// clean data dir before every test
			cleanDir(dataDir)

			// when we test only flags then there are no JSON or HCL
			// sources and we need to make only one pass over the
			// tests.
			flagsOnly := len(tt.json) == 0 && len(tt.hcl) == 0
			if flagsOnly && pass > 0 {
				continue
			}

			// json and hcl sources need to be in sync
			// to make sure we're generating the same config
			if len(tt.json) != len(tt.hcl) && !tt.skipformat {
				t.Fatal(tt.desc, ": JSON and HCL test case out of sync")
			}

			// select the source
			srcs, tails := tt.json, tt.jsontail
			if format == "hcl" {
				srcs, tails = tt.hcl, tt.hcltail
			}

			// If we're skipping a format and the current format is empty,
			// then skip it!
			if tt.skipformat && len(srcs) == 0 {
				continue
			}

			// build the description
			var desc []string
			if !flagsOnly {
				desc = append(desc, format)
			}
			if tt.desc != "" {
				desc = append(desc, tt.desc)
			}

			t.Run(strings.Join(desc, ":"), func(t *testing.T) {
				// first parse the flags
				flags := Flags{}
				fs := flag.NewFlagSet("", flag.ContinueOnError)
				AddFlags(fs, &flags)
				err := fs.Parse(tt.args)
				if err != nil {
					t.Fatalf("ParseFlags failed: %s", err)
				}
				flags.Args = fs.Args()

				if tt.pre != nil {
					tt.pre()
				}
				defer func() {
					if tt.post != nil {
						tt.post()
					}
				}()

				// Then create a builder with the flags.
				b, err := NewBuilder(flags)
				if err != nil {
					t.Fatal("NewBuilder", err)
				}

				// mock the hostname function unless a mock is provided
				b.Hostname = tt.hostname
				if b.Hostname == nil {
					b.Hostname = func() (string, error) { return "nodex", nil }
				}

				// mock the ip address detection
				privatev4 := tt.privatev4
				if privatev4 == nil {
					privatev4 = func() ([]*net.IPAddr, error) {
						return []*net.IPAddr{ipAddr("10.0.0.1")}, nil
					}
				}
				publicv6 := tt.publicv6
				if publicv6 == nil {
					publicv6 = func() ([]*net.IPAddr, error) {
						return []*net.IPAddr{ipAddr("dead:beef::1")}, nil
					}
				}
				b.GetPrivateIPv4 = privatev4
				b.GetPublicIPv6 = publicv6

				// read the source fragements
				for i, data := range srcs {
					b.Sources = append(b.Sources, Source{
						Name:   fmt.Sprintf("src-%d.%s", i, format),
						Format: format,
						Data:   data,
					})
				}
				for i, data := range tails {
					b.Tail = append(b.Tail, Source{
						Name:   fmt.Sprintf("tail-%d.%s", i, format),
						Format: format,
						Data:   data,
					})
				}

				// build/merge the config fragments
				rt, err := b.BuildAndValidate()
				if err == nil && tt.err != "" {
					t.Fatalf("got no error want %q", tt.err)
				}
				if err != nil && tt.err == "" {
					t.Fatalf("got error %s want nil", err)
				}
				if err == nil && tt.err != "" {
					t.Fatalf("got nil want error to contain %q", tt.err)
				}
				if err != nil && tt.err != "" && !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.err)
				}

				// check the warnings
				if !verify.Values(t, "warnings", b.Warnings, tt.warns) {
					t.FailNow()
				}

				// stop if we expected an error
				if tt.err != "" {
					return
				}

				// build a default configuration, then patch the fields we expect to change
				// and compare it with the generated configuration. Since the expected
				// runtime config has been validated we do not need to validate it again.
				x, err := NewBuilder(Flags{})
				if err != nil {
					t.Fatal(err)
				}
				x.Hostname = b.Hostname
				x.GetPrivateIPv4 = func() ([]*net.IPAddr, error) { return []*net.IPAddr{ipAddr("10.0.0.1")}, nil }
				x.GetPublicIPv6 = func() ([]*net.IPAddr, error) { return []*net.IPAddr{ipAddr("dead:beef::1")}, nil }
				patchedRT, err := x.Build()
				if err != nil {
					t.Fatalf("build default failed: %s", err)
				}
				if tt.patch != nil {
					tt.patch(&patchedRT)
				}
				// if err := x.Validate(wantRT); err != nil {
				// 	t.Fatalf("validate default failed: %s", err)
				// }
				if got, want := rt, patchedRT; !verify.Values(t, "", got, want) {
					t.FailNow()
				}
			})
		}
	}
}

// TestFullConfig tests the conversion from a fully populated JSON or
// HCL config file to a RuntimeConfig structure. All fields must be set
// to a unique non-zero value.
//
// To aid populating the fields the following bash functions can be used
// to generate random strings and ints:
//
//   random-int() { echo $RANDOM }
//   random-string() { base64 /dev/urandom | tr -d '/+' | fold -w ${1:-32} | head -n 1 }
//
// To generate a random string of length 8 run the following command in
// a terminal:
//
//   random-string 8
//
func TestFullConfig(t *testing.T) {
	dataDir := testutil.TempDir(t, "consul")
	defer os.RemoveAll(dataDir)

	cidr := func(s string) *net.IPNet {
		_, n, _ := net.ParseCIDR(s)
		return n
	}

	flagSrc := []string{`-dev`}
	src := map[string]string{
		"json": `{
			"acl_agent_master_token": "furuQD0b",
			"acl_agent_token": "cOshLOQ2",
			"acl_datacenter": "m3urck3z",
			"acl_default_policy": "ArK3WIfE",
			"acl_down_policy": "vZXMfMP0",
			"acl_enforce_version_8": true,
			"acl_enable_key_list_policy": true,
			"acl_master_token": "C1Q1oIwh",
			"acl_replication_token": "LMmgy5dO",
			"acl_token": "O1El0wan",
			"acl_ttl": "18060s",
			"acl" : {
				"enabled" : true,
				"down_policy" : "03eb2aee",
				"default_policy" : "72c2e7a0",
				"enable_key_list_policy": false,
				"enable_token_persistence": true,
				"policy_ttl": "1123s",
				"role_ttl": "9876s",
				"token_ttl": "3321s",
				"enable_token_replication" : true,
				"tokens" : {
					"master" : "8a19ac27",
					"agent_master" : "64fd0e08",
					"replication" : "5795983a",
					"agent" : "bed2377c",
					"default" : "418fdff1"
				}
			},
			"addresses": {
				"dns": "93.95.95.81",
				"http": "83.39.91.39",
				"https": "95.17.17.19",
				"grpc": "32.31.61.91"
			},
			"advertise_addr": "17.99.29.16",
			"advertise_addr_wan": "78.63.37.19",
			"autopilot": {
				"cleanup_dead_servers": true,
				"disable_upgrade_migration": true,
				"last_contact_threshold": "12705s",
				"max_trailing_logs": 17849,
				"redundancy_zone_tag": "3IsufDJf",
				"server_stabilization_time": "23057s",
				"upgrade_version_tag": "W9pDwFAL"
			},
			"bind_addr": "16.99.34.17",
			"bootstrap": true,
			"bootstrap_expect": 53,
			"ca_file": "erA7T0PM",
			"ca_path": "mQEN1Mfp",
			"cert_file": "7s4QAzDk",
			"check": {
				"id": "fZaCAXww",
				"name": "OOM2eo0f",
				"notes": "zXzXI9Gt",
				"service_id": "L8G0QNmR",
				"token": "oo4BCTgJ",
				"status": "qLykAl5u",
				"args": ["f3BemRjy", "e5zgpef7"],
				"http": "29B93haH",
				"header": {
					"hBq0zn1q": [ "2a9o9ZKP", "vKwA5lR6" ],
					"f3r6xFtM": [ "RyuIdDWv", "QbxEcIUM" ]
				},
				"method": "Dou0nGT5",
				"output_max_size": ` + strconv.Itoa(checks.DefaultBufSize) + `,
				"tcp": "JY6fTTcw",
				"interval": "18714s",
				"docker_container_id": "qF66POS9",
				"shell": "sOnDy228",
				"tls_skip_verify": true,
				"timeout": "5954s",
				"ttl": "30044s",
				"deregister_critical_service_after": "13209s"
			},
			"checks": [
				{
					"id": "uAjE6m9Z",
					"name": "QsZRGpYr",
					"notes": "VJ7Sk4BY",
					"service_id": "lSulPcyz",
					"token": "toO59sh8",
					"status": "9RlWsXMV",
					"args": ["4BAJttck", "4D2NPtTQ"],
					"http": "dohLcyQ2",
					"header": {
						"ZBfTin3L": [ "1sDbEqYG", "lJGASsWK" ],
						"Ui0nU99X": [ "LMccm3Qe", "k5H5RggQ" ]
					},
					"method": "aldrIQ4l",
					"tcp": "RJQND605",
					"interval": "22164s",
					"output_max_size": ` + strconv.Itoa(checks.DefaultBufSize) + `,
					"docker_container_id": "ipgdFtjd",
					"shell": "qAeOYy0M",
					"tls_skip_verify": true,
					"timeout": "1813s",
					"ttl": "21743s",
					"deregister_critical_service_after": "14232s"
				},
				{
					"id": "Cqq95BhP",
					"name": "3qXpkS0i",
					"notes": "sb5qLTex",
					"service_id": "CmUUcRna",
					"token": "a3nQzHuy",
					"status": "irj26nf3",
					"args": ["9s526ogY", "gSlOHj1w"],
					"http": "yzhgsQ7Y",
					"header": {
						"zcqwA8dO": [ "qb1zx0DL", "sXCxPFsD" ],
						"qxvdnSE9": [ "6wBPUYdF", "YYh8wtSZ" ]
					},
					"method": "gLrztrNw",
					"tcp": "4jG5casb",
					"interval": "28767s",
					"output_max_size": ` + strconv.Itoa(checks.DefaultBufSize) + `,
					"docker_container_id": "THW6u7rL",
					"shell": "C1Zt3Zwh",
					"tls_skip_verify": true,
					"timeout": "18506s",
					"ttl": "31006s",
					"deregister_critical_service_after": "2366s"
				}
			],
			"check_update_interval": "16507s",
			"client_addr": "93.83.18.19",
			"config_entries": {
				"bootstrap": [
					{
						"kind": "proxy-defaults",
						"name": "global",
						"config": {
							"foo": "bar",
							"bar": 1.0
						}
					}
				]
			},
			"connect": {
				"ca_provider": "consul",
				"ca_config": {
					"rotation_period": "90h",
					"leaf_cert_ttl": "1h",
					"csr_max_per_second": 100,
					"csr_max_concurrent": 2
				},
				"enabled": true,
				"proxy_defaults": {
					"exec_mode": "script",
					"daemon_command": ["consul", "connect", "proxy"],
					"script_command": ["proxyctl.sh"],
					"config": {
						"foo": "bar",
						"connect_timeout_ms": 1000,
						"pedantic_mode": true
					}
				}
			},
			"gossip_lan" : {
				"gossip_nodes": 6,
				"gossip_interval" : "25252s",
				"retransmit_mult" : 1234,
				"suspicion_mult"  : 1235,
				"probe_interval"  : "101ms",
				"probe_timeout"   : "102ms"
			},
			"gossip_wan" : {
				"gossip_nodes" : 2,
				"gossip_interval" : "6966s",
				"retransmit_mult" : 16384,
				"suspicion_mult"  : 16385,
				"probe_interval" : "103ms",
				"probe_timeout"  : "104ms"
			},
			"data_dir": "` + dataDir + `",
			"datacenter": "rzo029wg",
			"disable_anonymous_signature": true,
			"disable_coordinates": true,
			"disable_host_node_id": true,
			"disable_http_unprintable_char_filter": true,
			"disable_keyring_file": true,
			"disable_remote_exec": true,
			"disable_update_check": true,
			"discard_check_output": true,
			"discovery_max_stale": "5s",
			"domain": "7W1xXSqd",
			"dns_config": {
				"allow_stale": true,
				"a_record_limit": 29907,
				"disable_compression": true,
				"enable_truncate": true,
				"max_stale": "29685s",
				"node_ttl": "7084s",
				"only_passing": true,
				"recursor_timeout": "4427s",
				"service_ttl": {
					"*": "32030s"
				},
				"udp_answer_limit": 29909,
				"use_cache": true,
				"cache_max_age": "5m"
			},
			"enable_acl_replication": true,
			"enable_agent_tls_for_checks": true,
			"enable_central_service_config": true,
			"enable_debug": true,
			"enable_script_checks": true,
			"enable_local_script_checks": true,
			"enable_syslog": true,
			"encrypt": "A4wELWqH",
			"encrypt_verify_incoming": true,
			"encrypt_verify_outgoing": true,
			"http_config": {
				"block_endpoints": [ "RBvAFcGD", "fWOWFznh" ],
				"allow_write_http_from": [ "127.0.0.1/8", "22.33.44.55/32", "0.0.0.0/0" ],
				"response_headers": {
					"M6TKa9NP": "xjuxjOzQ",
					"JRCrHZed": "rl0mTx81"
				}
			},
			"key_file": "IEkkwgIA",
			"leave_on_terminate": true,
			"limits": {
				"rpc_rate": 12029.43,
				"rpc_max_burst": 44848
			},
			"log_level": "k1zo9Spt",
			"node_id": "AsUIlw99",
			"node_meta": {
				"5mgGQMBk": "mJLtVMSG",
				"A7ynFMJB": "0Nx6RGab"
			},
			"node_name": "otlLxGaI",
			"non_voting_server": true,
			"performance": {
				"leave_drain_time": "8265s",
				"raft_multiplier": 5,
				"rpc_hold_timeout": "15707s"
			},
			"pid_file": "43xN80Km",
			"ports": {
				"dns": 7001,
				"http": 7999,
				"https": 15127,
				"server": 3757,
				"grpc": 4881,
				"proxy_min_port": 2000,
				"proxy_max_port": 3000,
				"sidecar_min_port": 8888,
				"sidecar_max_port": 9999
			},
			"protocol": 30793,
			"primary_datacenter": "ejtmd43d",
			"raft_protocol": 19016,
			"raft_snapshot_threshold": 16384,
			"raft_snapshot_interval": "30s",
			"reconnect_timeout": "23739s",
			"reconnect_timeout_wan": "26694s",
			"recursors": [ "63.38.39.58", "92.49.18.18" ],
			"rejoin_after_leave": true,
			"retry_interval": "8067s",
			"retry_interval_wan": "28866s",
			"retry_join": [ "pbsSFY7U", "l0qLtWij" ],
			"retry_join_wan": [ "PFsR02Ye", "rJdQIhER" ],
			"retry_max": 913,
			"retry_max_wan": 23160,
			"segment": "BC2NhTDi",
			"segments": [
				{
					"name": "PExYMe2E",
					"bind": "36.73.36.19",
					"port": 38295,
					"rpc_listener": true,
					"advertise": "63.39.19.18"
				},
				{
					"name": "UzCvJgup",
					"bind": "37.58.38.19",
					"port": 39292,
					"rpc_listener": true,
					"advertise": "83.58.26.27"
				}
			],
			"serf_lan": "99.43.63.15",
			"serf_wan": "67.88.33.19",
			"server": true,
			"server_name": "Oerr9n1G",
			"service": {
				"id": "dLOXpSCI",
				"name": "o1ynPkp0",
				"meta": {
					"mymeta": "data"
				},
				"tags": ["nkwshvM5", "NTDWn3ek"],
				"address": "cOlSOhbp",
				"token": "msy7iWER",
				"port": 24237,
				"weights": {
					"passing": 100,
					"warning": 1
				},
				"enable_tag_override": true,
				"check": {
					"id": "RMi85Dv8",
					"name": "iehanzuq",
					"status": "rCvn53TH",
					"notes": "fti5lfF3",
					"args": ["16WRUmwS", "QWk7j7ae"],
					"http": "dl3Fgme3",
					"header": {
						"rjm4DEd3": ["2m3m2Fls"],
						"l4HwQ112": ["fk56MNlo", "dhLK56aZ"]
					},
					"method": "9afLm3Mj",
					"tcp": "fjiLFqVd",
					"interval": "23926s",
					"output_max_size": ` + strconv.Itoa(checks.DefaultBufSize) + `,
					"docker_container_id": "dO5TtRHk",
					"shell": "e6q2ttES",
					"tls_skip_verify": true,
					"timeout": "38483s",
					"ttl": "10943s",
					"deregister_critical_service_after": "68787s"
				},
				"checks": [
					{
						"id": "Zv99e9Ka",
						"name": "sgV4F7Pk",
						"notes": "yP5nKbW0",
						"status": "7oLMEyfu",
						"args": ["5wEZtZpv", "0Ihyk8cS"],
						"http": "KyDjGY9H",
						"header": {
							"gv5qefTz": [ "5Olo2pMG", "PvvKWQU5" ],
							"SHOVq1Vv": [ "jntFhyym", "GYJh32pp" ]
						},
						"method": "T66MFBfR",
						"tcp": "bNnNfx2A",
						"interval": "22224s",
						"output_max_size": ` + strconv.Itoa(checks.DefaultBufSize) + `,
						"docker_container_id": "ipgdFtjd",
						"shell": "omVZq7Sz",
						"tls_skip_verify": true,
						"timeout": "18913s",
						"ttl": "44743s",
						"deregister_critical_service_after": "8482s"
					},
					{
						"id": "G79O6Mpr",
						"name": "IEqrzrsd",
						"notes": "SVqApqeM",
						"status": "XXkVoZXt",
						"args": ["wD05Bvao", "rLYB7kQC"],
						"http": "kyICZsn8",
						"header": {
							"4ebP5vL4": [ "G20SrL5Q", "DwPKlMbo" ],
							"p2UI34Qz": [ "UsG1D0Qh", "NHhRiB6s" ]
						},
						"method": "ciYHWors",
						"tcp": "FfvCwlqH",
						"interval": "12356s",
						"output_max_size": ` + strconv.Itoa(checks.DefaultBufSize) + `,
						"docker_container_id": "HBndBU6R",
						"shell": "hVI33JjA",
						"tls_skip_verify": true,
						"timeout": "38282s",
						"ttl": "1181s",
						"deregister_critical_service_after": "4992s"
					}
				],
				"connect": {
					"native": true
				}
			},
			"services": [
				{
					"id": "wI1dzxS4",
					"name": "7IszXMQ1",
					"tags": ["0Zwg8l6v", "zebELdN5"],
					"address": "9RhqPSPB",
					"token": "myjKJkWH",
					"port": 72219,
					"enable_tag_override": true,
					"check": {
						"id": "qmfeO5if",
						"name": "atDGP7n5",
						"status": "pDQKEhWL",
						"notes": "Yt8EDLev",
						"args": ["81EDZLPa", "bPY5X8xd"],
						"http": "qzHYvmJO",
						"header": {
							"UkpmZ3a3": ["2dfzXuxZ"],
							"cVFpko4u": ["gGqdEB6k", "9LsRo22u"]
						},
						"method": "X5DrovFc",
						"tcp": "ICbxkpSF",
						"interval": "24392s",
						"output_max_size": ` + strconv.Itoa(checks.DefaultBufSize) + `,
						"docker_container_id": "ZKXr68Yb",
						"shell": "CEfzx0Fo",
						"tls_skip_verify": true,
						"timeout": "38333s",
						"ttl": "57201s",
						"deregister_critical_service_after": "44214s"
					},
					"connect": {
						"sidecar_service": {}
					}
				},
				{
					"id": "MRHVMZuD",
					"name": "6L6BVfgH",
					"tags": ["7Ale4y6o", "PMBW08hy"],
					"address": "R6H6g8h0",
					"token": "ZgY8gjMI",
					"port": 38292,
					"weights": {
						"passing": 1979,
						"warning": 6
					},
					"enable_tag_override": true,
					"checks": [
						{
							"id": "GTti9hCo",
							"name": "9OOS93ne",
							"notes": "CQy86DH0",
							"status": "P0SWDvrk",
							"args": ["EXvkYIuG", "BATOyt6h"],
							"http": "u97ByEiW",
							"header": {
								"MUlReo8L": [ "AUZG7wHG", "gsN0Dc2N" ],
								"1UJXjVrT": [ "OJgxzTfk", "xZZrFsq7" ]
							},
							"method": "5wkAxCUE",
							"tcp": "MN3oA9D2",
							"interval": "32718s",
							"output_max_size": ` + strconv.Itoa(checks.DefaultBufSize) + `,
							"docker_container_id": "cU15LMet",
							"shell": "nEz9qz2l",
							"tls_skip_verify": true,
							"timeout": "34738s",
							"ttl": "22773s",
							"deregister_critical_service_after": "84282s"
						},
						{
							"id": "UHsDeLxG",
							"name": "PQSaPWlT",
							"notes": "jKChDOdl",
							"status": "5qFz6OZn",
							"args": ["NMtYWlT9", "vj74JXsm"],
							"http": "1LBDJhw4",
							"header": {
								"cXPmnv1M": [ "imDqfaBx", "NFxZ1bQe" ],
								"vr7wY7CS": [ "EtCoNPPL", "9vAarJ5s" ]
							},
							"method": "wzByP903",
							"tcp": "2exjZIGE",
							"interval": "5656s",
							"output_max_size": ` + strconv.Itoa(checks.DefaultBufSize) + `,
							"docker_container_id": "5tDBWpfA",
							"shell": "rlTpLM8s",
							"tls_skip_verify": true,
							"timeout": "4868s",
							"ttl": "11222s",
							"deregister_critical_service_after": "68482s"
						}
					],
					"connect": {
						"proxy": {
							"exec_mode": "daemon",
							"command": ["awesome-proxy"],
							"config": {
								"foo": "qux"
							}
						}
					}
				},
				{
					"id": "Kh81CPF6",
					"kind": "connect-proxy",
					"name": "Kh81CPF6-proxy",
					"port": 31471,
					"proxy": {
						"config": {
								"cedGGtZf": "pWrUNiWw"
						},
						"destination_service_id": "6L6BVfgH-id",
						"destination_service_name": "6L6BVfgH",
						"local_service_address": "127.0.0.2",
						"local_service_port": 23759,
						"upstreams": [
							{
								"destination_name": "KPtAj2cb",
								"local_bind_port": 4051,
								"config": {
									"kzRnZOyd": "nUNKoL8H"
								}
							},
							{
								"destination_name": "KSd8HsRl",
								"destination_namespace": "9nakw0td",
								"destination_type": "prepared_query",
								"local_bind_address": "127.24.88.0",
								"local_bind_port": 11884
							}
						]
					}
				}
			],
			"session_ttl_min": "26627s",
			"skip_leave_on_interrupt": true,
			"start_join": [ "LR3hGDoG", "MwVpZ4Up" ],
			"start_join_wan": [ "EbFSc3nA", "kwXTh623" ],
			"syslog_facility": "hHv79Uia",
			"tagged_addresses": {
				"7MYgHrYH": "dALJAhLD",
				"h6DdBy6K": "ebrr9zZ8"
			},
			"telemetry": {
				"circonus_api_app": "p4QOTe9j",
				"circonus_api_token": "E3j35V23",
				"circonus_api_url": "mEMjHpGg",
				"circonus_broker_id": "BHlxUhed",
				"circonus_broker_select_tag": "13xy1gHm",
				"circonus_check_display_name": "DRSlQR6n",
				"circonus_check_force_metric_activation": "Ua5FGVYf",
				"circonus_check_id": "kGorutad",
				"circonus_check_instance_id": "rwoOL6R4",
				"circonus_check_search_tag": "ovT4hT4f",
				"circonus_check_tags": "prvO4uBl",
				"circonus_submission_interval": "DolzaflP",
				"circonus_submission_url": "gTcbS93G",
				"disable_hostname": true,
				"dogstatsd_addr": "0wSndumK",
				"dogstatsd_tags": [ "3N81zSUB","Xtj8AnXZ" ],
				"filter_default": true,
				"prefix_filter": [ "+oJotS8XJ","-cazlEhGn" ],
				"metrics_prefix": "ftO6DySn",
				"prometheus_retention_time": "15s",
				"statsd_address": "drce87cy",
				"statsite_address": "HpFwKB8R"
			},
			"tls_cipher_suites": "TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384",
			"tls_min_version": "pAOWafkR",
			"tls_prefer_server_cipher_suites": true,
			"translate_wan_addrs": true,
			"ui": true,
			"ui_dir": "11IFzAUn",
			"unix_sockets": {
				"group": "8pFodrV8",
				"mode": "E8sAwOv4",
				"user": "E0nB1DwA"
			},
			"verify_incoming": true,
			"verify_incoming_https": true,
			"verify_incoming_rpc": true,
			"verify_outgoing": true,
			"verify_server_hostname": true,
			"watches": [
				{
					"type": "key",
					"datacenter": "GyE6jpeW",
					"key": "j9lF1Tve",
					"handler": "90N7S4LN"
				}, {
					"type": "keyprefix",
					"datacenter": "fYrl3F5d",
					"key": "sl3Dffu7",
					"args": ["dltjDJ2a", "flEa7C2d"]
				}
			]
		}`,
		"hcl": `
			acl_agent_master_token = "furuQD0b"
			acl_agent_token = "cOshLOQ2"
			acl_datacenter = "m3urck3z"
			acl_default_policy = "ArK3WIfE"
			acl_down_policy = "vZXMfMP0"
			acl_enforce_version_8 = true
			acl_enable_key_list_policy = true
			acl_master_token = "C1Q1oIwh"
			acl_replication_token = "LMmgy5dO"
			acl_token = "O1El0wan"
			acl_ttl = "18060s"
			acl = {
				enabled = true
				down_policy = "03eb2aee"
				default_policy = "72c2e7a0"
				enable_key_list_policy = false
				enable_token_persistence = true
				policy_ttl = "1123s"
				role_ttl = "9876s"
				token_ttl = "3321s"
				enable_token_replication = true
				tokens = {
					master = "8a19ac27",
					agent_master = "64fd0e08",
					replication = "5795983a",
					agent = "bed2377c",
					default = "418fdff1"
				}
			}
			addresses = {
				dns = "93.95.95.81"
				http = "83.39.91.39"
				https = "95.17.17.19"
				grpc = "32.31.61.91"
			}
			advertise_addr = "17.99.29.16"
			advertise_addr_wan = "78.63.37.19"
			autopilot = {
				cleanup_dead_servers = true
				disable_upgrade_migration = true
				last_contact_threshold = "12705s"
				max_trailing_logs = 17849
				redundancy_zone_tag = "3IsufDJf"
				server_stabilization_time = "23057s"
				upgrade_version_tag = "W9pDwFAL"
			}
			bind_addr = "16.99.34.17"
			bootstrap = true
			bootstrap_expect = 53
			ca_file = "erA7T0PM"
			ca_path = "mQEN1Mfp"
			cert_file = "7s4QAzDk"
			check = {
				id = "fZaCAXww"
				name = "OOM2eo0f"
				notes = "zXzXI9Gt"
				service_id = "L8G0QNmR"
				token = "oo4BCTgJ"
				status = "qLykAl5u"
				args = ["f3BemRjy", "e5zgpef7"]
				http = "29B93haH"
				header = {
					hBq0zn1q = [ "2a9o9ZKP", "vKwA5lR6" ]
					f3r6xFtM = [ "RyuIdDWv", "QbxEcIUM" ]
				}
				method = "Dou0nGT5"
				tcp = "JY6fTTcw"
				interval = "18714s"
				output_max_size = ` + strconv.Itoa(checks.DefaultBufSize) + `
				docker_container_id = "qF66POS9"
				shell = "sOnDy228"
				tls_skip_verify = true
				timeout = "5954s"
				ttl = "30044s"
				deregister_critical_service_after = "13209s"
			},
			checks = [
				{
					id = "uAjE6m9Z"
					name = "QsZRGpYr"
					notes = "VJ7Sk4BY"
					service_id = "lSulPcyz"
					token = "toO59sh8"
					status = "9RlWsXMV"
					args = ["4BAJttck", "4D2NPtTQ"]
					http = "dohLcyQ2"
					header = {
						"ZBfTin3L" = [ "1sDbEqYG", "lJGASsWK" ]
						"Ui0nU99X" = [ "LMccm3Qe", "k5H5RggQ" ]
					}
					method = "aldrIQ4l"
					tcp = "RJQND605"
					interval = "22164s"
					output_max_size = ` + strconv.Itoa(checks.DefaultBufSize) + `
					docker_container_id = "ipgdFtjd"
					shell = "qAeOYy0M"
					tls_skip_verify = true
					timeout = "1813s"
					ttl = "21743s"
					deregister_critical_service_after = "14232s"
				},
				{
					id = "Cqq95BhP"
					name = "3qXpkS0i"
					notes = "sb5qLTex"
					service_id = "CmUUcRna"
					token = "a3nQzHuy"
					status = "irj26nf3"
					args = ["9s526ogY", "gSlOHj1w"]
					http = "yzhgsQ7Y"
					header = {
						"zcqwA8dO" = [ "qb1zx0DL", "sXCxPFsD" ]
						"qxvdnSE9" = [ "6wBPUYdF", "YYh8wtSZ" ]
					}
					method = "gLrztrNw"
					tcp = "4jG5casb"
					interval = "28767s"
					output_max_size = ` + strconv.Itoa(checks.DefaultBufSize) + `
					docker_container_id = "THW6u7rL"
					shell = "C1Zt3Zwh"
					tls_skip_verify = true
					timeout = "18506s"
					ttl = "31006s"
					deregister_critical_service_after = "2366s"
				}
			]
			check_update_interval = "16507s"
			client_addr = "93.83.18.19"
			config_entries {
				# This is using the repeated block-to-array HCL magic
				bootstrap {
					kind = "proxy-defaults"
					name = "global"
					config {
						foo = "bar"
						bar = 1.0
					}
				}
			}
			connect {
				ca_provider = "consul"
				ca_config {
					rotation_period = "90h"
					leaf_cert_ttl = "1h"
					# hack float since json parses numbers as float and we have to
					# assert against the same thing
					csr_max_per_second = 100.0
					csr_max_concurrent = 2.0
				}
				enabled = true
				proxy_defaults {
					exec_mode = "script"
					daemon_command = ["consul", "connect", "proxy"]
					script_command = ["proxyctl.sh"]
					config = {
						foo = "bar"
						# hack float since json parses numbers as float and we have to
						# assert against the same thing
						connect_timeout_ms = 1000.0
						pedantic_mode = true
					}
				}
			}
			gossip_lan {
				gossip_nodes    = 6
				gossip_interval = "25252s"
				retransmit_mult = 1234
				suspicion_mult  = 1235
				probe_interval  = "101ms"
				probe_timeout   = "102ms"
			}
			gossip_wan {
				gossip_nodes    = 2
				gossip_interval = "6966s"
				retransmit_mult = 16384
				suspicion_mult  = 16385
				probe_interval  = "103ms"
				probe_timeout   = "104ms"
			}
			data_dir = "` + dataDir + `"
			datacenter = "rzo029wg"
			disable_anonymous_signature = true
			disable_coordinates = true
			disable_host_node_id = true
			disable_http_unprintable_char_filter = true
			disable_keyring_file = true
			disable_remote_exec = true
			disable_update_check = true
			discard_check_output = true
			discovery_max_stale = "5s"
			domain = "7W1xXSqd"
			dns_config {
				allow_stale = true
				a_record_limit = 29907
				disable_compression = true
				enable_truncate = true
				max_stale = "29685s"
				node_ttl = "7084s"
				only_passing = true
				recursor_timeout = "4427s"
				service_ttl = {
					"*" = "32030s"
				}
				udp_answer_limit = 29909
				use_cache = true
				cache_max_age = "5m"
			}
			enable_acl_replication = true
			enable_agent_tls_for_checks = true
			enable_central_service_config = true
			enable_debug = true
			enable_script_checks = true
			enable_local_script_checks = true
			enable_syslog = true
			encrypt = "A4wELWqH"
			encrypt_verify_incoming = true
			encrypt_verify_outgoing = true
			http_config {
				block_endpoints = [ "RBvAFcGD", "fWOWFznh" ]
				allow_write_http_from = [ "127.0.0.1/8", "22.33.44.55/32", "0.0.0.0/0" ]
				response_headers = {
					"M6TKa9NP" = "xjuxjOzQ"
					"JRCrHZed" = "rl0mTx81"
				}
			}
			key_file = "IEkkwgIA"
			leave_on_terminate = true
			limits {
				rpc_rate = 12029.43
				rpc_max_burst = 44848
			}
			log_level = "k1zo9Spt"
			node_id = "AsUIlw99"
			node_meta {
				"5mgGQMBk" = "mJLtVMSG"
				"A7ynFMJB" = "0Nx6RGab"
			}
			node_name = "otlLxGaI"
			non_voting_server = true
			performance {
				leave_drain_time = "8265s"
				raft_multiplier = 5
				rpc_hold_timeout = "15707s"
			}
			pid_file = "43xN80Km"
			ports {
				dns = 7001,
				http = 7999,
				https = 15127
				server = 3757
				grpc = 4881
				proxy_min_port = 2000
				proxy_max_port = 3000
				sidecar_min_port = 8888
				sidecar_max_port = 9999
			}
			protocol = 30793
			primary_datacenter = "ejtmd43d"
			raft_protocol = 19016
			raft_snapshot_threshold = 16384
			raft_snapshot_interval = "30s"
			reconnect_timeout = "23739s"
			reconnect_timeout_wan = "26694s"
			recursors = [ "63.38.39.58", "92.49.18.18" ]
			rejoin_after_leave = true
			retry_interval = "8067s"
			retry_interval_wan = "28866s"
			retry_join = [ "pbsSFY7U", "l0qLtWij" ]
			retry_join_wan = [ "PFsR02Ye", "rJdQIhER" ]
			retry_max = 913
			retry_max_wan = 23160
			segment = "BC2NhTDi"
			segments = [
				{
					name = "PExYMe2E"
					bind = "36.73.36.19"
					port = 38295
					rpc_listener = true
					advertise = "63.39.19.18"
				},
				{
					name = "UzCvJgup"
					bind = "37.58.38.19"
					port = 39292
					rpc_listener = true
					advertise = "83.58.26.27"
				}
			]
			serf_lan = "99.43.63.15"
			serf_wan = "67.88.33.19"
			server = true
			server_name = "Oerr9n1G"
			service = {
				id = "dLOXpSCI"
				name = "o1ynPkp0"
				meta = {
					mymeta = "data"
				}
				tags = ["nkwshvM5", "NTDWn3ek"]
				address = "cOlSOhbp"
				token = "msy7iWER"
				port = 24237
				weights = {
					passing = 100,
					warning = 1
				}
				enable_tag_override = true
				check = {
					id = "RMi85Dv8"
					name = "iehanzuq"
					status = "rCvn53TH"
					notes = "fti5lfF3"
					args = ["16WRUmwS", "QWk7j7ae"]
					http = "dl3Fgme3"
					header = {
						rjm4DEd3 = [ "2m3m2Fls" ]
						l4HwQ112 = [ "fk56MNlo", "dhLK56aZ" ]
					}
					method = "9afLm3Mj"
					tcp = "fjiLFqVd"
					interval = "23926s"
					docker_container_id = "dO5TtRHk"
					shell = "e6q2ttES"
					tls_skip_verify = true
					timeout = "38483s"
					ttl = "10943s"
					deregister_critical_service_after = "68787s"
				}
				checks = [
					{
						id = "Zv99e9Ka"
						name = "sgV4F7Pk"
						notes = "yP5nKbW0"
						status = "7oLMEyfu"
						args = ["5wEZtZpv", "0Ihyk8cS"]
						http = "KyDjGY9H"
						header = {
							"gv5qefTz" = [ "5Olo2pMG", "PvvKWQU5" ]
							"SHOVq1Vv" = [ "jntFhyym", "GYJh32pp" ]
						}
						method = "T66MFBfR"
						tcp = "bNnNfx2A"
						interval = "22224s"
						output_max_size = ` + strconv.Itoa(checks.DefaultBufSize) + `
						docker_container_id = "ipgdFtjd"
						shell = "omVZq7Sz"
						tls_skip_verify = true
						timeout = "18913s"
						ttl = "44743s"
						deregister_critical_service_after = "8482s"
					},
					{
						id = "G79O6Mpr"
						name = "IEqrzrsd"
						notes = "SVqApqeM"
						status = "XXkVoZXt"
						args = ["wD05Bvao", "rLYB7kQC"]
						http = "kyICZsn8"
						header = {
							"4ebP5vL4" = [ "G20SrL5Q", "DwPKlMbo" ]
							"p2UI34Qz" = [ "UsG1D0Qh", "NHhRiB6s" ]
						}
						method = "ciYHWors"
						tcp = "FfvCwlqH"
						interval = "12356s"
						output_max_size = ` + strconv.Itoa(checks.DefaultBufSize) + `
						docker_container_id = "HBndBU6R"
						shell = "hVI33JjA"
						tls_skip_verify = true
						timeout = "38282s"
						ttl = "1181s"
						deregister_critical_service_after = "4992s"
					}
				]
				connect {
					native = true
				}
			}
			services = [
				{
					id = "wI1dzxS4"
					name = "7IszXMQ1"
					tags = ["0Zwg8l6v", "zebELdN5"]
					address = "9RhqPSPB"
					token = "myjKJkWH"
					port = 72219
					enable_tag_override = true
					check = {
						id = "qmfeO5if"
						name = "atDGP7n5"
						status = "pDQKEhWL"
						notes = "Yt8EDLev"
						args = ["81EDZLPa", "bPY5X8xd"]
						http = "qzHYvmJO"
						header = {
							UkpmZ3a3 = [ "2dfzXuxZ" ]
							cVFpko4u = [ "gGqdEB6k", "9LsRo22u" ]
						}
						method = "X5DrovFc"
						tcp = "ICbxkpSF"
						interval = "24392s"
						output_max_size = ` + strconv.Itoa(checks.DefaultBufSize) + `
						docker_container_id = "ZKXr68Yb"
						shell = "CEfzx0Fo"
						tls_skip_verify = true
						timeout = "38333s"
						ttl = "57201s"
						deregister_critical_service_after = "44214s"
					}
					connect {
						sidecar_service {}
					}
				},
				{
					id = "MRHVMZuD"
					name = "6L6BVfgH"
					tags = ["7Ale4y6o", "PMBW08hy"]
					address = "R6H6g8h0"
					token = "ZgY8gjMI"
					port = 38292
					weights = {
						passing = 1979,
						warning = 6
					}
					enable_tag_override = true
					checks = [
						{
							id = "GTti9hCo"
							name = "9OOS93ne"
							notes = "CQy86DH0"
							status = "P0SWDvrk"
							args = ["EXvkYIuG", "BATOyt6h"]
							http = "u97ByEiW"
							header = {
								"MUlReo8L" = [ "AUZG7wHG", "gsN0Dc2N" ]
								"1UJXjVrT" = [ "OJgxzTfk", "xZZrFsq7" ]
							}
							method = "5wkAxCUE"
							tcp = "MN3oA9D2"
							interval = "32718s"
							output_max_size = ` + strconv.Itoa(checks.DefaultBufSize) + `
							docker_container_id = "cU15LMet"
							shell = "nEz9qz2l"
							tls_skip_verify = true
							timeout = "34738s"
							ttl = "22773s"
							deregister_critical_service_after = "84282s"
						},
						{
							id = "UHsDeLxG"
							name = "PQSaPWlT"
							notes = "jKChDOdl"
							status = "5qFz6OZn"
							args = ["NMtYWlT9", "vj74JXsm"]
							http = "1LBDJhw4"
							header = {
								"cXPmnv1M" = [ "imDqfaBx", "NFxZ1bQe" ],
								"vr7wY7CS" = [ "EtCoNPPL", "9vAarJ5s" ]
							}
							method = "wzByP903"
							tcp = "2exjZIGE"
							interval = "5656s"
							output_max_size = ` + strconv.Itoa(checks.DefaultBufSize) + `
							docker_container_id = "5tDBWpfA"
							shell = "rlTpLM8s"
							tls_skip_verify = true
							timeout = "4868s"
							ttl = "11222s"
							deregister_critical_service_after = "68482s"
						}
					]
					connect {
						proxy {
							exec_mode = "daemon"
							command = ["awesome-proxy"]
							config = {
								foo = "qux"
							}
						}
					}
				},
				{
					id = "Kh81CPF6"
					name = "Kh81CPF6-proxy"
					port = 31471
					kind = "connect-proxy"
					proxy {
						destination_service_name = "6L6BVfgH"
						destination_service_id = "6L6BVfgH-id"
						local_service_address = "127.0.0.2"
						local_service_port = 23759
						config {
							cedGGtZf = "pWrUNiWw"
						}
						upstreams = [
							{
								destination_name = "KPtAj2cb"
								local_bind_port = 4051
								config {
									kzRnZOyd = "nUNKoL8H"
								}
							},
							{
								destination_type = "prepared_query"
								destination_namespace = "9nakw0td"
								destination_name = "KSd8HsRl"
								local_bind_port = 11884
								local_bind_address = "127.24.88.0"
							},
						]
					}
				}
			]
			session_ttl_min = "26627s"
			skip_leave_on_interrupt = true
			start_join = [ "LR3hGDoG", "MwVpZ4Up" ]
			start_join_wan = [ "EbFSc3nA", "kwXTh623" ]
			syslog_facility = "hHv79Uia"
			tagged_addresses = {
				"7MYgHrYH" = "dALJAhLD"
				"h6DdBy6K" = "ebrr9zZ8"
			}
			telemetry {
				circonus_api_app = "p4QOTe9j"
				circonus_api_token = "E3j35V23"
				circonus_api_url = "mEMjHpGg"
				circonus_broker_id = "BHlxUhed"
				circonus_broker_select_tag = "13xy1gHm"
				circonus_check_display_name = "DRSlQR6n"
				circonus_check_force_metric_activation = "Ua5FGVYf"
				circonus_check_id = "kGorutad"
				circonus_check_instance_id = "rwoOL6R4"
				circonus_check_search_tag = "ovT4hT4f"
				circonus_check_tags = "prvO4uBl"
				circonus_submission_interval = "DolzaflP"
				circonus_submission_url = "gTcbS93G"
				disable_hostname = true
				dogstatsd_addr = "0wSndumK"
				dogstatsd_tags = [ "3N81zSUB","Xtj8AnXZ" ]
				filter_default = true
				prefix_filter = [ "+oJotS8XJ","-cazlEhGn" ]
				metrics_prefix = "ftO6DySn"
				prometheus_retention_time = "15s"
				statsd_address = "drce87cy"
				statsite_address = "HpFwKB8R"
			}
			tls_cipher_suites = "TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384"
			tls_min_version = "pAOWafkR"
			tls_prefer_server_cipher_suites = true
			translate_wan_addrs = true
			ui = true
			ui_dir = "11IFzAUn"
			unix_sockets = {
				group = "8pFodrV8"
				mode = "E8sAwOv4"
				user = "E0nB1DwA"
			}
			verify_incoming = true
			verify_incoming_https = true
			verify_incoming_rpc = true
			verify_outgoing = true
			verify_server_hostname = true
			watches = [{
				type = "key"
				datacenter = "GyE6jpeW"
				key = "j9lF1Tve"
				handler = "90N7S4LN"
			}, {
				type = "keyprefix"
				datacenter = "fYrl3F5d"
				key = "sl3Dffu7"
				args = ["dltjDJ2a", "flEa7C2d"]
			}]
		`}

	tail := map[string][]Source{
		"json": []Source{
			{
				Name:   "tail.non-user.json",
				Format: "json",
				Data: `
				{
					"acl_disabled_ttl": "957s",
					"acl" : {
						"disabled_ttl" : "957s"
					},
					"ae_interval": "10003s",
					"check_deregister_interval_min": "27870s",
					"check_reap_interval": "10662s",
					"discovery_max_stale": "5s",
					"segment_limit": 24705,
					"segment_name_limit": 27046,
					"sync_coordinate_interval_min": "27983s",
					"sync_coordinate_rate_target": 137.81
				}`,
			},
			{
				Name:   "tail.consul.json",
				Format: "json",
				Data: `
				{
					"consul": {
						"coordinate": {
							"update_batch_size": 9244,
							"update_max_batches": 15164,
							"update_period": "25093s"
						},
						"raft": {
							"election_timeout": "31947s",
							"heartbeat_timeout": "25699s",
							"leader_lease_timeout": "15351s"
						},
						"server": {
							"health_interval": "17455s"
						}
					}
				}`,
			},
		},
		"hcl": []Source{
			{
				Name:   "tail.non-user.hcl",
				Format: "hcl",
				Data: `
					acl_disabled_ttl = "957s"
					acl = {
						disabled_ttl = "957s"
					}
					ae_interval = "10003s"
					check_deregister_interval_min = "27870s"
					check_reap_interval = "10662s"
					discovery_max_stale = "5s"
					segment_limit = 24705
					segment_name_limit = 27046
					sync_coordinate_interval_min = "27983s"
					sync_coordinate_rate_target = 137.81
				`,
			},
			{
				Name:   "tail.consul.hcl",
				Format: "hcl",
				Data: `
					consul = {
						coordinate = {
							update_batch_size = 9244
							update_max_batches = 15164
							update_period = "25093s"
						}
						raft = {
							election_timeout = "31947s"
							heartbeat_timeout = "25699s"
							leader_lease_timeout = "15351s"
						}
						server = {
							health_interval = "17455s"
						}
					}
				`,
			},
		},
	}

	want := RuntimeConfig{
		// non-user configurable values
		ACLDisabledTTL:             957 * time.Second,
		AEInterval:                 10003 * time.Second,
		CheckDeregisterIntervalMin: 27870 * time.Second,
		CheckReapInterval:          10662 * time.Second,
		SegmentLimit:               24705,
		SegmentNameLimit:           27046,
		SyncCoordinateIntervalMin:  27983 * time.Second,
		SyncCoordinateRateTarget:   137.81,

		Revision:          "JNtPSav3",
		Version:           "R909Hblt",
		VersionPrerelease: "ZT1JOQLn",

		// consul configuration
		ConsulCoordinateUpdateBatchSize:  9244,
		ConsulCoordinateUpdateMaxBatches: 15164,
		ConsulCoordinateUpdatePeriod:     25093 * time.Second,
		ConsulRaftElectionTimeout:        5 * 31947 * time.Second,
		ConsulRaftHeartbeatTimeout:       5 * 25699 * time.Second,
		ConsulRaftLeaderLeaseTimeout:     5 * 15351 * time.Second,
		GossipLANGossipInterval:          25252 * time.Second,
		GossipLANGossipNodes:             6,
		GossipLANProbeInterval:           101 * time.Millisecond,
		GossipLANProbeTimeout:            102 * time.Millisecond,
		GossipLANSuspicionMult:           1235,
		GossipLANRetransmitMult:          1234,
		GossipWANGossipInterval:          6966 * time.Second,
		GossipWANGossipNodes:             2,
		GossipWANProbeInterval:           103 * time.Millisecond,
		GossipWANProbeTimeout:            104 * time.Millisecond,
		GossipWANSuspicionMult:           16385,
		GossipWANRetransmitMult:          16384,
		ConsulServerHealthInterval:       17455 * time.Second,

		// user configurable values

		ACLAgentMasterToken:              "64fd0e08",
		ACLAgentToken:                    "bed2377c",
		ACLsEnabled:                      true,
		ACLDatacenter:                    "ejtmd43d",
		ACLDefaultPolicy:                 "72c2e7a0",
		ACLDownPolicy:                    "03eb2aee",
		ACLEnforceVersion8:               true,
		ACLEnableKeyListPolicy:           false,
		ACLEnableTokenPersistence:        true,
		ACLMasterToken:                   "8a19ac27",
		ACLReplicationToken:              "5795983a",
		ACLTokenTTL:                      3321 * time.Second,
		ACLPolicyTTL:                     1123 * time.Second,
		ACLRoleTTL:                       9876 * time.Second,
		ACLToken:                         "418fdff1",
		ACLTokenReplication:              true,
		AdvertiseAddrLAN:                 ipAddr("17.99.29.16"),
		AdvertiseAddrWAN:                 ipAddr("78.63.37.19"),
		AutopilotCleanupDeadServers:      true,
		AutopilotDisableUpgradeMigration: true,
		AutopilotLastContactThreshold:    12705 * time.Second,
		AutopilotMaxTrailingLogs:         17849,
		AutopilotRedundancyZoneTag:       "3IsufDJf",
		AutopilotServerStabilizationTime: 23057 * time.Second,
		AutopilotUpgradeVersionTag:       "W9pDwFAL",
		BindAddr:                         ipAddr("16.99.34.17"),
		Bootstrap:                        true,
		BootstrapExpect:                  53,
		CAFile:                           "erA7T0PM",
		CAPath:                           "mQEN1Mfp",
		CertFile:                         "7s4QAzDk",
		CheckOutputMaxSize:               checks.DefaultBufSize,
		Checks: []*structs.CheckDefinition{
			&structs.CheckDefinition{
				ID:         "uAjE6m9Z",
				Name:       "QsZRGpYr",
				Notes:      "VJ7Sk4BY",
				ServiceID:  "lSulPcyz",
				Token:      "toO59sh8",
				Status:     "9RlWsXMV",
				ScriptArgs: []string{"4BAJttck", "4D2NPtTQ"},
				HTTP:       "dohLcyQ2",
				Header: map[string][]string{
					"ZBfTin3L": []string{"1sDbEqYG", "lJGASsWK"},
					"Ui0nU99X": []string{"LMccm3Qe", "k5H5RggQ"},
				},
				Method:                         "aldrIQ4l",
				TCP:                            "RJQND605",
				Interval:                       22164 * time.Second,
				OutputMaxSize:                  checks.DefaultBufSize,
				DockerContainerID:              "ipgdFtjd",
				Shell:                          "qAeOYy0M",
				TLSSkipVerify:                  true,
				Timeout:                        1813 * time.Second,
				TTL:                            21743 * time.Second,
				DeregisterCriticalServiceAfter: 14232 * time.Second,
			},
			&structs.CheckDefinition{
				ID:         "Cqq95BhP",
				Name:       "3qXpkS0i",
				Notes:      "sb5qLTex",
				ServiceID:  "CmUUcRna",
				Token:      "a3nQzHuy",
				Status:     "irj26nf3",
				ScriptArgs: []string{"9s526ogY", "gSlOHj1w"},
				HTTP:       "yzhgsQ7Y",
				Header: map[string][]string{
					"zcqwA8dO": []string{"qb1zx0DL", "sXCxPFsD"},
					"qxvdnSE9": []string{"6wBPUYdF", "YYh8wtSZ"},
				},
				Method:                         "gLrztrNw",
				OutputMaxSize:                  checks.DefaultBufSize,
				TCP:                            "4jG5casb",
				Interval:                       28767 * time.Second,
				DockerContainerID:              "THW6u7rL",
				Shell:                          "C1Zt3Zwh",
				TLSSkipVerify:                  true,
				Timeout:                        18506 * time.Second,
				TTL:                            31006 * time.Second,
				DeregisterCriticalServiceAfter: 2366 * time.Second,
			},
			&structs.CheckDefinition{
				ID:         "fZaCAXww",
				Name:       "OOM2eo0f",
				Notes:      "zXzXI9Gt",
				ServiceID:  "L8G0QNmR",
				Token:      "oo4BCTgJ",
				Status:     "qLykAl5u",
				ScriptArgs: []string{"f3BemRjy", "e5zgpef7"},
				HTTP:       "29B93haH",
				Header: map[string][]string{
					"hBq0zn1q": {"2a9o9ZKP", "vKwA5lR6"},
					"f3r6xFtM": {"RyuIdDWv", "QbxEcIUM"},
				},
				Method:                         "Dou0nGT5",
				OutputMaxSize:                  checks.DefaultBufSize,
				TCP:                            "JY6fTTcw",
				Interval:                       18714 * time.Second,
				DockerContainerID:              "qF66POS9",
				Shell:                          "sOnDy228",
				TLSSkipVerify:                  true,
				Timeout:                        5954 * time.Second,
				TTL:                            30044 * time.Second,
				DeregisterCriticalServiceAfter: 13209 * time.Second,
			},
		},
		CheckUpdateInterval: 16507 * time.Second,
		ClientAddrs:         []*net.IPAddr{ipAddr("93.83.18.19")},
		ConfigEntryBootstrap: []structs.ConfigEntry{
			&structs.ProxyConfigEntry{
				Kind: structs.ProxyDefaults,
				Name: structs.ProxyConfigGlobal,
				Config: map[string]interface{}{
					"foo": "bar",
					// has to be a float due to being a map[string]interface
					"bar": float64(1),
				},
			},
		},
		ConnectEnabled:          true,
		ConnectProxyBindMinPort: 2000,
		ConnectProxyBindMaxPort: 3000,
		ConnectSidecarMinPort:   8888,
		ConnectSidecarMaxPort:   9999,
		ConnectCAProvider:       "consul",
		ConnectCAConfig: map[string]interface{}{
			"RotationPeriod":   "90h",
			"LeafCertTTL":      "1h",
			"CSRMaxPerSecond":  float64(100),
			"CSRMaxConcurrent": float64(2),
		},
		ConnectProxyAllowManagedRoot:            false,
		ConnectProxyAllowManagedAPIRegistration: false,
		ConnectProxyDefaultExecMode:             "script",
		ConnectProxyDefaultDaemonCommand:        []string{"consul", "connect", "proxy"},
		ConnectProxyDefaultScriptCommand:        []string{"proxyctl.sh"},
		ConnectProxyDefaultConfig: map[string]interface{}{
			"foo":                "bar",
			"connect_timeout_ms": float64(1000),
			"pedantic_mode":      true,
		},
		DNSAddrs:                         []net.Addr{tcpAddr("93.95.95.81:7001"), udpAddr("93.95.95.81:7001")},
		DNSARecordLimit:                  29907,
		DNSAllowStale:                    true,
		DNSDisableCompression:            true,
		DNSDomain:                        "7W1xXSqd",
		DNSEnableTruncate:                true,
		DNSMaxStale:                      29685 * time.Second,
		DNSNodeTTL:                       7084 * time.Second,
		DNSOnlyPassing:                   true,
		DNSPort:                          7001,
		DNSRecursorTimeout:               4427 * time.Second,
		DNSRecursors:                     []string{"63.38.39.58", "92.49.18.18"},
		DNSSOA:                           RuntimeSOAConfig{Refresh: 3600, Retry: 600, Expire: 86400, Minttl: 0},
		DNSServiceTTL:                    map[string]time.Duration{"*": 32030 * time.Second},
		DNSUDPAnswerLimit:                29909,
		DNSNodeMetaTXT:                   true,
		DNSUseCache:                      true,
		DNSCacheMaxAge:                   5 * time.Minute,
		DataDir:                          dataDir,
		Datacenter:                       "rzo029wg",
		DevMode:                          true,
		DisableAnonymousSignature:        true,
		DisableCoordinates:               true,
		DisableHostNodeID:                true,
		DisableHTTPUnprintableCharFilter: true,
		DisableKeyringFile:               true,
		DisableRemoteExec:                true,
		DisableUpdateCheck:               true,
		DiscardCheckOutput:               true,
		DiscoveryMaxStale:                5 * time.Second,
		EnableAgentTLSForChecks:          true,
		EnableCentralServiceConfig:       true,
		EnableDebug:                      true,
		EnableRemoteScriptChecks:         true,
		EnableLocalScriptChecks:          true,
		EnableSyslog:                     true,
		EnableUI:                         true,
		EncryptKey:                       "A4wELWqH",
		EncryptVerifyIncoming:            true,
		EncryptVerifyOutgoing:            true,
		GRPCPort:                         4881,
		GRPCAddrs:                        []net.Addr{tcpAddr("32.31.61.91:4881")},
		HTTPAddrs:                        []net.Addr{tcpAddr("83.39.91.39:7999")},
		HTTPBlockEndpoints:               []string{"RBvAFcGD", "fWOWFznh"},
		AllowWriteHTTPFrom:               []*net.IPNet{cidr("127.0.0.0/8"), cidr("22.33.44.55/32"), cidr("0.0.0.0/0")},
		HTTPPort:                         7999,
		HTTPResponseHeaders:              map[string]string{"M6TKa9NP": "xjuxjOzQ", "JRCrHZed": "rl0mTx81"},
		HTTPSAddrs:                       []net.Addr{tcpAddr("95.17.17.19:15127")},
		HTTPSPort:                        15127,
		KeyFile:                          "IEkkwgIA",
		LeaveDrainTime:                   8265 * time.Second,
		LeaveOnTerm:                      true,
		LogLevel:                         "k1zo9Spt",
		NodeID:                           types.NodeID("AsUIlw99"),
		NodeMeta:                         map[string]string{"5mgGQMBk": "mJLtVMSG", "A7ynFMJB": "0Nx6RGab"},
		NodeName:                         "otlLxGaI",
		NonVotingServer:                  true,
		PidFile:                          "43xN80Km",
		PrimaryDatacenter:                "ejtmd43d",
		RPCAdvertiseAddr:                 tcpAddr("17.99.29.16:3757"),
		RPCBindAddr:                      tcpAddr("16.99.34.17:3757"),
		RPCHoldTimeout:                   15707 * time.Second,
		RPCProtocol:                      30793,
		RPCRateLimit:                     12029.43,
		RPCMaxBurst:                      44848,
		RaftProtocol:                     19016,
		RaftSnapshotThreshold:            16384,
		RaftSnapshotInterval:             30 * time.Second,
		ReconnectTimeoutLAN:              23739 * time.Second,
		ReconnectTimeoutWAN:              26694 * time.Second,
		RejoinAfterLeave:                 true,
		RetryJoinIntervalLAN:             8067 * time.Second,
		RetryJoinIntervalWAN:             28866 * time.Second,
		RetryJoinLAN:                     []string{"pbsSFY7U", "l0qLtWij"},
		RetryJoinMaxAttemptsLAN:          913,
		RetryJoinMaxAttemptsWAN:          23160,
		RetryJoinWAN:                     []string{"PFsR02Ye", "rJdQIhER"},
		SegmentName:                      "BC2NhTDi",
		Segments: []structs.NetworkSegment{
			{
				Name:        "PExYMe2E",
				Bind:        tcpAddr("36.73.36.19:38295"),
				Advertise:   tcpAddr("63.39.19.18:38295"),
				RPCListener: true,
			},
			{
				Name:        "UzCvJgup",
				Bind:        tcpAddr("37.58.38.19:39292"),
				Advertise:   tcpAddr("83.58.26.27:39292"),
				RPCListener: true,
			},
		},
		SerfPortLAN: 8301,
		SerfPortWAN: 8302,
		ServerMode:  true,
		ServerName:  "Oerr9n1G",
		ServerPort:  3757,
		Services: []*structs.ServiceDefinition{
			{
				ID:      "wI1dzxS4",
				Name:    "7IszXMQ1",
				Tags:    []string{"0Zwg8l6v", "zebELdN5"},
				Address: "9RhqPSPB",
				Token:   "myjKJkWH",
				Port:    72219,
				Weights: &structs.Weights{
					Passing: 1,
					Warning: 1,
				},
				EnableTagOverride: true,
				Checks: []*structs.CheckType{
					&structs.CheckType{
						CheckID:    "qmfeO5if",
						Name:       "atDGP7n5",
						Status:     "pDQKEhWL",
						Notes:      "Yt8EDLev",
						ScriptArgs: []string{"81EDZLPa", "bPY5X8xd"},
						HTTP:       "qzHYvmJO",
						Header: map[string][]string{
							"UkpmZ3a3": {"2dfzXuxZ"},
							"cVFpko4u": {"gGqdEB6k", "9LsRo22u"},
						},
						Method:                         "X5DrovFc",
						OutputMaxSize:                  checks.DefaultBufSize,
						TCP:                            "ICbxkpSF",
						Interval:                       24392 * time.Second,
						DockerContainerID:              "ZKXr68Yb",
						Shell:                          "CEfzx0Fo",
						TLSSkipVerify:                  true,
						Timeout:                        38333 * time.Second,
						TTL:                            57201 * time.Second,
						DeregisterCriticalServiceAfter: 44214 * time.Second,
					},
				},
				// Note that although this SidecarService is only syntax sugar for
				// registering another service, that has to happen in the agent code so
				// it can make intelligent decisions about automatic port assignments
				// etc. So we expect config just to pass it through verbatim.
				Connect: &structs.ServiceConnect{
					SidecarService: &structs.ServiceDefinition{
						Weights: &structs.Weights{
							Passing: 1,
							Warning: 1,
						},
					},
				},
			},
			{
				ID:      "MRHVMZuD",
				Name:    "6L6BVfgH",
				Tags:    []string{"7Ale4y6o", "PMBW08hy"},
				Address: "R6H6g8h0",
				Token:   "ZgY8gjMI",
				Port:    38292,
				Weights: &structs.Weights{
					Passing: 1979,
					Warning: 6,
				},
				EnableTagOverride: true,
				Checks: structs.CheckTypes{
					&structs.CheckType{
						CheckID:    "GTti9hCo",
						Name:       "9OOS93ne",
						Notes:      "CQy86DH0",
						Status:     "P0SWDvrk",
						ScriptArgs: []string{"EXvkYIuG", "BATOyt6h"},
						HTTP:       "u97ByEiW",
						Header: map[string][]string{
							"MUlReo8L": {"AUZG7wHG", "gsN0Dc2N"},
							"1UJXjVrT": {"OJgxzTfk", "xZZrFsq7"},
						},
						Method:                         "5wkAxCUE",
						OutputMaxSize:                  checks.DefaultBufSize,
						TCP:                            "MN3oA9D2",
						Interval:                       32718 * time.Second,
						DockerContainerID:              "cU15LMet",
						Shell:                          "nEz9qz2l",
						TLSSkipVerify:                  true,
						Timeout:                        34738 * time.Second,
						TTL:                            22773 * time.Second,
						DeregisterCriticalServiceAfter: 84282 * time.Second,
					},
					&structs.CheckType{
						CheckID:    "UHsDeLxG",
						Name:       "PQSaPWlT",
						Notes:      "jKChDOdl",
						Status:     "5qFz6OZn",
						ScriptArgs: []string{"NMtYWlT9", "vj74JXsm"},
						HTTP:       "1LBDJhw4",
						Header: map[string][]string{
							"cXPmnv1M": {"imDqfaBx", "NFxZ1bQe"},
							"vr7wY7CS": {"EtCoNPPL", "9vAarJ5s"},
						},
						Method:                         "wzByP903",
						OutputMaxSize:                  checks.DefaultBufSize,
						TCP:                            "2exjZIGE",
						Interval:                       5656 * time.Second,
						DockerContainerID:              "5tDBWpfA",
						Shell:                          "rlTpLM8s",
						TLSSkipVerify:                  true,
						Timeout:                        4868 * time.Second,
						TTL:                            11222 * time.Second,
						DeregisterCriticalServiceAfter: 68482 * time.Second,
					},
				},
				Connect: &structs.ServiceConnect{
					Proxy: &structs.ServiceDefinitionConnectProxy{
						ExecMode: "daemon",
						Command:  []string{"awesome-proxy"},
						Config: map[string]interface{}{
							"foo": "qux",
						},
					},
				},
			},
			{
				ID:   "Kh81CPF6",
				Name: "Kh81CPF6-proxy",
				Port: 31471,
				Kind: "connect-proxy",
				Proxy: &structs.ConnectProxyConfig{
					DestinationServiceName: "6L6BVfgH",
					DestinationServiceID:   "6L6BVfgH-id",
					LocalServiceAddress:    "127.0.0.2",
					LocalServicePort:       23759,
					Config: map[string]interface{}{
						"cedGGtZf": "pWrUNiWw",
					},
					Upstreams: structs.Upstreams{
						{
							DestinationType: "service", // Default should be explicitly filled
							DestinationName: "KPtAj2cb",
							LocalBindPort:   4051,
							Config: map[string]interface{}{
								"kzRnZOyd": "nUNKoL8H",
							},
						},
						{
							DestinationType:      "prepared_query",
							DestinationNamespace: "9nakw0td",
							DestinationName:      "KSd8HsRl",
							LocalBindPort:        11884,
							LocalBindAddress:     "127.24.88.0",
						},
					},
				},
				Weights: &structs.Weights{
					Passing: 1,
					Warning: 1,
				},
			},
			{
				ID:      "dLOXpSCI",
				Name:    "o1ynPkp0",
				Tags:    []string{"nkwshvM5", "NTDWn3ek"},
				Address: "cOlSOhbp",
				Token:   "msy7iWER",
				Meta:    map[string]string{"mymeta": "data"},
				Port:    24237,
				Weights: &structs.Weights{
					Passing: 100,
					Warning: 1,
				},
				EnableTagOverride: true,
				Connect: &structs.ServiceConnect{
					Native: true,
				},
				Checks: structs.CheckTypes{
					&structs.CheckType{
						CheckID:    "Zv99e9Ka",
						Name:       "sgV4F7Pk",
						Notes:      "yP5nKbW0",
						Status:     "7oLMEyfu",
						ScriptArgs: []string{"5wEZtZpv", "0Ihyk8cS"},
						HTTP:       "KyDjGY9H",
						Header: map[string][]string{
							"gv5qefTz": {"5Olo2pMG", "PvvKWQU5"},
							"SHOVq1Vv": {"jntFhyym", "GYJh32pp"},
						},
						Method:                         "T66MFBfR",
						OutputMaxSize:                  checks.DefaultBufSize,
						TCP:                            "bNnNfx2A",
						Interval:                       22224 * time.Second,
						DockerContainerID:              "ipgdFtjd",
						Shell:                          "omVZq7Sz",
						TLSSkipVerify:                  true,
						Timeout:                        18913 * time.Second,
						TTL:                            44743 * time.Second,
						DeregisterCriticalServiceAfter: 8482 * time.Second,
					},
					&structs.CheckType{
						CheckID:    "G79O6Mpr",
						Name:       "IEqrzrsd",
						Notes:      "SVqApqeM",
						Status:     "XXkVoZXt",
						ScriptArgs: []string{"wD05Bvao", "rLYB7kQC"},
						HTTP:       "kyICZsn8",
						Header: map[string][]string{
							"4ebP5vL4": {"G20SrL5Q", "DwPKlMbo"},
							"p2UI34Qz": {"UsG1D0Qh", "NHhRiB6s"},
						},
						Method:                         "ciYHWors",
						OutputMaxSize:                  checks.DefaultBufSize,
						TCP:                            "FfvCwlqH",
						Interval:                       12356 * time.Second,
						DockerContainerID:              "HBndBU6R",
						Shell:                          "hVI33JjA",
						TLSSkipVerify:                  true,
						Timeout:                        38282 * time.Second,
						TTL:                            1181 * time.Second,
						DeregisterCriticalServiceAfter: 4992 * time.Second,
					},
					&structs.CheckType{
						CheckID:    "RMi85Dv8",
						Name:       "iehanzuq",
						Status:     "rCvn53TH",
						Notes:      "fti5lfF3",
						ScriptArgs: []string{"16WRUmwS", "QWk7j7ae"},
						HTTP:       "dl3Fgme3",
						Header: map[string][]string{
							"rjm4DEd3": {"2m3m2Fls"},
							"l4HwQ112": {"fk56MNlo", "dhLK56aZ"},
						},
						Method:                         "9afLm3Mj",
						OutputMaxSize:                  checks.DefaultBufSize,
						TCP:                            "fjiLFqVd",
						Interval:                       23926 * time.Second,
						DockerContainerID:              "dO5TtRHk",
						Shell:                          "e6q2ttES",
						TLSSkipVerify:                  true,
						Timeout:                        38483 * time.Second,
						TTL:                            10943 * time.Second,
						DeregisterCriticalServiceAfter: 68787 * time.Second,
					},
				},
			},
		},
		SerfAdvertiseAddrLAN: tcpAddr("17.99.29.16:8301"),
		SerfAdvertiseAddrWAN: tcpAddr("78.63.37.19:8302"),
		SerfBindAddrLAN:      tcpAddr("99.43.63.15:8301"),
		SerfBindAddrWAN:      tcpAddr("67.88.33.19:8302"),
		SessionTTLMin:        26627 * time.Second,
		SkipLeaveOnInt:       true,
		StartJoinAddrsLAN:    []string{"LR3hGDoG", "MwVpZ4Up"},
		StartJoinAddrsWAN:    []string{"EbFSc3nA", "kwXTh623"},
		SyslogFacility:       "hHv79Uia",
		Telemetry: lib.TelemetryConfig{
			CirconusAPIApp:                     "p4QOTe9j",
			CirconusAPIToken:                   "E3j35V23",
			CirconusAPIURL:                     "mEMjHpGg",
			CirconusBrokerID:                   "BHlxUhed",
			CirconusBrokerSelectTag:            "13xy1gHm",
			CirconusCheckDisplayName:           "DRSlQR6n",
			CirconusCheckForceMetricActivation: "Ua5FGVYf",
			CirconusCheckID:                    "kGorutad",
			CirconusCheckInstanceID:            "rwoOL6R4",
			CirconusCheckSearchTag:             "ovT4hT4f",
			CirconusCheckTags:                  "prvO4uBl",
			CirconusSubmissionInterval:         "DolzaflP",
			CirconusSubmissionURL:              "gTcbS93G",
			DisableHostname:                    true,
			DogstatsdAddr:                      "0wSndumK",
			DogstatsdTags:                      []string{"3N81zSUB", "Xtj8AnXZ"},
			FilterDefault:                      true,
			AllowedPrefixes:                    []string{"oJotS8XJ"},
			BlockedPrefixes:                    []string{"cazlEhGn"},
			MetricsPrefix:                      "ftO6DySn",
			PrometheusRetentionTime:            15 * time.Second,
			StatsdAddr:                         "drce87cy",
			StatsiteAddr:                       "HpFwKB8R",
		},
		TLSCipherSuites:             []uint16{tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305, tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384},
		TLSMinVersion:               "pAOWafkR",
		TLSPreferServerCipherSuites: true,
		TaggedAddresses: map[string]string{
			"7MYgHrYH": "dALJAhLD",
			"h6DdBy6K": "ebrr9zZ8",
			"lan":      "17.99.29.16",
			"wan":      "78.63.37.19",
		},
		TranslateWANAddrs:    true,
		UIContentPath:        "/ui/",
		UIDir:                "11IFzAUn",
		UnixSocketUser:       "E0nB1DwA",
		UnixSocketGroup:      "8pFodrV8",
		UnixSocketMode:       "E8sAwOv4",
		VerifyIncoming:       true,
		VerifyIncomingHTTPS:  true,
		VerifyIncomingRPC:    true,
		VerifyOutgoing:       true,
		VerifyServerHostname: true,
		Watches: []map[string]interface{}{
			map[string]interface{}{
				"type":       "key",
				"datacenter": "GyE6jpeW",
				"key":        "j9lF1Tve",
				"handler":    "90N7S4LN",
			},
			map[string]interface{}{
				"type":       "keyprefix",
				"datacenter": "fYrl3F5d",
				"key":        "sl3Dffu7",
				"args":       []interface{}{"dltjDJ2a", "flEa7C2d"},
			},
		},
	}

	warns := []string{
		`The 'acl_datacenter' field is deprecated. Use the 'primary_datacenter' field instead.`,
		`bootstrap_expect > 0: expecting 53 servers`,
	}

	// ensure that all fields are set to unique non-zero values
	// todo(fs): This currently fails since ServiceDefinition.Check is not used
	// todo(fs): not sure on how to work around this. Possible options are:
	// todo(fs):  * move first check into the Check field
	// todo(fs):  * ignore the Check field
	// todo(fs): both feel like a hack
	if err := nonZero("RuntimeConfig", nil, want); err != nil {
		t.Log(err)
	}

	for format, data := range src {
		t.Run(format, func(t *testing.T) {
			// parse the flags since this is the only way we can set the
			// DevMode flag
			var flags Flags
			fs := flag.NewFlagSet("", flag.ContinueOnError)
			AddFlags(fs, &flags)
			if err := fs.Parse(flagSrc); err != nil {
				t.Fatalf("ParseFlags: %s", err)
			}

			// ensure that all fields are set to unique non-zero values
			// if err := nonZero("Config", nil, c); err != nil {
			// 	t.Fatal(err)
			// }

			b, err := NewBuilder(flags)
			if err != nil {
				t.Fatalf("NewBuilder: %s", err)
			}
			b.Sources = append(b.Sources, Source{Name: "full." + format, Data: data})
			b.Tail = append(b.Tail, tail[format]...)
			b.Tail = append(b.Tail, VersionSource("JNtPSav3", "R909Hblt", "ZT1JOQLn"))

			// construct the runtime config
			rt, err := b.Build()
			if err != nil {
				t.Fatalf("Build: %s", err)
			}

			// verify that all fields are set
			if !verify.Values(t, "runtime_config", rt, want) {
				t.FailNow()
			}

			// at this point we have confirmed that the parsing worked
			// for all fields but the validation will fail since certain
			// combinations are not allowed. Since it is not possible to have
			// all fields with non-zero values and to have a valid configuration
			// we are patching a handful of safe fields to make validation pass.
			rt.Bootstrap = false
			rt.DevMode = false
			rt.EnableUI = false
			rt.SegmentName = ""
			rt.Segments = nil

			// validate the runtime config
			if err := b.Validate(rt); err != nil {
				t.Fatalf("Validate: %s", err)
			}

			// check the warnings
			if got, want := b.Warnings, warns; !verify.Values(t, "warnings", got, want) {
				t.FailNow()
			}
		})
	}
}

// nonZero verifies recursively that all fields are set to unique,
// non-zero and non-nil values.
//
// struct: check all fields recursively
// slice: check len > 0 and all values recursively
// ptr: check not nil
// bool: check not zero (cannot check uniqueness)
// string, int, uint: check not zero and unique
// other: error
func nonZero(name string, uniq map[interface{}]string, v interface{}) error {
	if v == nil {
		return fmt.Errorf("%q is nil", name)
	}

	if uniq == nil {
		uniq = map[interface{}]string{}
	}

	isUnique := func(v interface{}) error {
		if other := uniq[v]; other != "" {
			return fmt.Errorf("%q and %q both use value %q", name, other, v)
		}
		uniq[v] = name
		return nil
	}

	val, typ := reflect.ValueOf(v), reflect.TypeOf(v)
	// fmt.Printf("%s: %T\n", name, v)
	switch typ.Kind() {
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			fieldname := fmt.Sprintf("%s.%s", name, f.Name)
			err := nonZero(fieldname, uniq, val.Field(i).Interface())
			if err != nil {
				return err
			}
		}

	case reflect.Slice:
		if val.Len() == 0 {
			return fmt.Errorf("%q is empty slice", name)
		}
		for i := 0; i < val.Len(); i++ {
			elemname := fmt.Sprintf("%s[%d]", name, i)
			err := nonZero(elemname, uniq, val.Index(i).Interface())
			if err != nil {
				return err
			}
		}

	case reflect.Map:
		if val.Len() == 0 {
			return fmt.Errorf("%q is empty map", name)
		}
		for _, key := range val.MapKeys() {
			keyname := fmt.Sprintf("%s[%s]", name, key.String())
			if err := nonZero(keyname, uniq, key.Interface()); err != nil {
				if strings.Contains(err.Error(), "is zero value") {
					return fmt.Errorf("%q has zero value map key", name)
				}
				return err
			}
			if err := nonZero(keyname, uniq, val.MapIndex(key).Interface()); err != nil {
				return err
			}
		}

	case reflect.Bool:
		if val.Bool() != true {
			return fmt.Errorf("%q is zero value", name)
		}
		// do not test bool for uniqueness since there are only two values

	case reflect.String:
		if val.Len() == 0 {
			return fmt.Errorf("%q is zero value", name)
		}
		return isUnique(v)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if val.Int() == 0 {
			return fmt.Errorf("%q is zero value", name)
		}
		return isUnique(v)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if val.Uint() == 0 {
			return fmt.Errorf("%q is zero value", name)
		}
		return isUnique(v)

	case reflect.Float32, reflect.Float64:
		if val.Float() == 0 {
			return fmt.Errorf("%q is zero value", name)
		}
		return isUnique(v)

	case reflect.Ptr:
		if val.IsNil() {
			return fmt.Errorf("%q is nil", name)
		}
		return nonZero("*"+name, uniq, val.Elem().Interface())

	default:
		return fmt.Errorf("%T is not supported", v)
	}
	return nil
}

func TestNonZero(t *testing.T) {
	var empty string

	tests := []struct {
		desc string
		v    interface{}
		err  error
	}{
		{"nil", nil, errors.New(`"x" is nil`)},
		{"zero bool", false, errors.New(`"x" is zero value`)},
		{"zero string", "", errors.New(`"x" is zero value`)},
		{"zero int", int(0), errors.New(`"x" is zero value`)},
		{"zero int8", int8(0), errors.New(`"x" is zero value`)},
		{"zero int16", int16(0), errors.New(`"x" is zero value`)},
		{"zero int32", int32(0), errors.New(`"x" is zero value`)},
		{"zero int64", int64(0), errors.New(`"x" is zero value`)},
		{"zero uint", uint(0), errors.New(`"x" is zero value`)},
		{"zero uint8", uint8(0), errors.New(`"x" is zero value`)},
		{"zero uint16", uint16(0), errors.New(`"x" is zero value`)},
		{"zero uint32", uint32(0), errors.New(`"x" is zero value`)},
		{"zero uint64", uint64(0), errors.New(`"x" is zero value`)},
		{"zero float32", float32(0), errors.New(`"x" is zero value`)},
		{"zero float64", float64(0), errors.New(`"x" is zero value`)},
		{"ptr to zero value", &empty, errors.New(`"*x" is zero value`)},
		{"empty slice", []string{}, errors.New(`"x" is empty slice`)},
		{"slice with zero value", []string{""}, errors.New(`"x[0]" is zero value`)},
		{"empty map", map[string]string{}, errors.New(`"x" is empty map`)},
		{"map with zero value key", map[string]string{"": "y"}, errors.New(`"x" has zero value map key`)},
		{"map with zero value elem", map[string]string{"y": ""}, errors.New(`"x[y]" is zero value`)},
		{"struct with nil field", struct{ Y *int }{}, errors.New(`"x.Y" is nil`)},
		{"struct with zero value field", struct{ Y string }{}, errors.New(`"x.Y" is zero value`)},
		{"struct with empty array", struct{ Y []string }{}, errors.New(`"x.Y" is empty slice`)},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, want := nonZero("x", nil, tt.v), tt.err; !reflect.DeepEqual(got, want) {
				t.Fatalf("got error %v want %v", got, want)
			}
		})
	}
}

func TestConfigDecodeBytes(t *testing.T) {
	t.Parallel()
	// Test with some input
	src := []byte("abc")
	key := base64.StdEncoding.EncodeToString(src)

	result, err := decodeBytes(key)
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	if !bytes.Equal(src, result) {
		t.Fatalf("bad: %#v", result)
	}

	// Test with no input
	result, err = decodeBytes("")
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	if len(result) > 0 {
		t.Fatalf("bad: %#v", result)
	}
}

func TestSanitize(t *testing.T) {
	rt := RuntimeConfig{
		BindAddr:             &net.IPAddr{IP: net.ParseIP("127.0.0.1")},
		CheckOutputMaxSize:   checks.DefaultBufSize,
		SerfAdvertiseAddrLAN: &net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 5678},
		DNSAddrs: []net.Addr{
			&net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 5678},
			&net.UDPAddr{IP: net.ParseIP("1.2.3.4"), Port: 5678},
		},
		DNSSOA: RuntimeSOAConfig{Refresh: 3600, Retry: 600, Expire: 86400, Minttl: 0},
		HTTPAddrs: []net.Addr{
			&net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 5678},
			&net.UnixAddr{Name: "/var/run/foo"},
		},
		ConsulCoordinateUpdatePeriod: 15 * time.Second,
		RetryJoinLAN: []string{
			"foo=bar key=baz secret=boom bang=bar",
		},
		RetryJoinWAN: []string{
			"wan_foo=bar wan_key=baz wan_secret=boom wan_bang=bar",
		},
		Services: []*structs.ServiceDefinition{
			&structs.ServiceDefinition{
				Name:  "foo",
				Token: "bar",
				Check: structs.CheckType{
					Name:          "blurb",
					OutputMaxSize: checks.DefaultBufSize,
				},
				Weights: &structs.Weights{
					Passing: 67,
					Warning: 3,
				},
			},
		},
		Checks: []*structs.CheckDefinition{
			&structs.CheckDefinition{
				Name:          "zoo",
				Token:         "zope",
				OutputMaxSize: checks.DefaultBufSize,
			},
		},
	}

	rtJSON := `{
		"ACLAgentMasterToken": "hidden",
		"ACLAgentToken": "hidden",
		"ACLDatacenter": "",
		"ACLDefaultPolicy": "",
		"ACLDisabledTTL": "0s",
		"ACLDownPolicy": "",
		"ACLEnableKeyListPolicy": false,
		"ACLEnableTokenPersistence": false,
		"ACLEnforceVersion8": false,
		"ACLMasterToken": "hidden",
		"ACLPolicyTTL": "0s",
		"ACLReplicationToken": "hidden",
		"ACLRoleTTL": "0s",
		"ACLTokenReplication": false,
		"ACLTokenTTL": "0s",
		"ACLToken": "hidden",
		"ACLsEnabled": false,
		"AEInterval": "0s",
		"AdvertiseAddrLAN": "",
		"AdvertiseAddrWAN": "",
		"AutopilotCleanupDeadServers": false,
		"AutopilotDisableUpgradeMigration": false,
		"AutopilotLastContactThreshold": "0s",
		"AutopilotMaxTrailingLogs": 0,
		"AutopilotRedundancyZoneTag": "",
		"AutopilotServerStabilizationTime": "0s",
		"AutopilotUpgradeVersionTag": "",
		"BindAddr": "127.0.0.1",
		"Bootstrap": false,
		"BootstrapExpect": 0,
		"CAFile": "",
		"CAPath": "",
		"CertFile": "",
		"CheckDeregisterIntervalMin": "0s",
		"CheckOutputMaxSize": ` + strconv.Itoa(checks.DefaultBufSize) + `,
		"CheckReapInterval": "0s",
		"CheckUpdateInterval": "0s",
		"Checks": [{
			"AliasNode": "",
			"AliasService": "",
			"DeregisterCriticalServiceAfter": "0s",
			"DockerContainerID": "",
			"GRPC": "",
			"GRPCUseTLS": false,
			"HTTP": "",
			"Header": {},
			"ID": "",
			"Interval": "0s",
			"Method": "",
			"Name": "zoo",
			"Notes": "",
			"OutputMaxSize": ` + strconv.Itoa(checks.DefaultBufSize) + `,
			"ScriptArgs": [],
			"ServiceID": "",
			"Shell": "",
			"Status": "",
			"TCP": "",
			"TLSSkipVerify": false,
			"TTL": "0s",
			"Timeout": "0s",
			"Token": "hidden"
		}],
		"ClientAddrs": [],
		"ConfigEntryBootstrap": [],
		"ConnectCAConfig": {},
		"ConnectCAProvider": "",
		"ConnectEnabled": false,
		"ConnectProxyAllowManagedAPIRegistration": false,
		"ConnectProxyAllowManagedRoot": false,
		"ConnectProxyBindMaxPort": 0,
		"ConnectProxyBindMinPort": 0,
		"ConnectProxyDefaultConfig": {},
		"ConnectProxyDefaultDaemonCommand": [],
		"ConnectProxyDefaultExecMode": "",
		"ConnectProxyDefaultScriptCommand": [],
		"ConnectSidecarMaxPort": 0,
		"ConnectSidecarMinPort": 0,
		"ConnectTestCALeafRootChangeSpread": "0s",
		"ConnectTestDisableManagedProxies": false,
		"ConsulCoordinateUpdateBatchSize": 0,
		"ConsulCoordinateUpdateMaxBatches": 0,
		"ConsulCoordinateUpdatePeriod": "15s",
		"ConsulRaftElectionTimeout": "0s",
		"CheckOutputMaxSize": ` + strconv.Itoa(checks.DefaultBufSize) + `,
		"ConsulRaftHeartbeatTimeout": "0s",
		"ConsulRaftLeaderLeaseTimeout": "0s",
		"GossipLANGossipInterval": "0s",
		"GossipLANGossipNodes": 0,
		"GossipLANProbeInterval": "0s",
		"GossipLANProbeTimeout": "0s",
		"GossipLANRetransmitMult": 0,
		"GossipLANSuspicionMult": 0,
		"GossipWANGossipInterval": "0s",
		"GossipWANGossipNodes": 0,
		"GossipWANProbeInterval": "0s",
		"GossipWANProbeTimeout": "0s",
		"GossipWANRetransmitMult": 0,
		"GossipWANSuspicionMult": 0,
		"ConsulServerHealthInterval": "0s",
		"DNSARecordLimit": 0,
		"DNSAddrs": [
			"tcp://1.2.3.4:5678",
			"udp://1.2.3.4:5678"
		],
		"DNSAllowStale": false,
		"DNSDisableCompression": false,
		"DNSDomain": "",
		"DNSEnableTruncate": false,
		"DNSMaxStale": "0s",
		"DNSNodeMetaTXT": false,
		"DNSNodeTTL": "0s",
		"DNSOnlyPassing": false,
		"DNSPort": 0,
		"DNSRecursorTimeout": "0s",
		"DNSRecursors": [],
		"DNSServiceTTL": {},
		"DNSSOA": {
			"Refresh": 3600,
			"Retry": 600,
			"Expire": 86400,
			"Minttl": 0
		},
		"DNSUDPAnswerLimit": 0,
		"DNSUseCache": false,
		"DNSCacheMaxAge": "0s",
		"DataDir": "",
		"Datacenter": "",
		"DevMode": false,
		"DisableAnonymousSignature": false,
		"DisableCoordinates": false,
		"DisableHTTPUnprintableCharFilter": false,
		"DisableHostNodeID": false,
		"DisableKeyringFile": false,
		"DisableRemoteExec": false,
		"DisableUpdateCheck": false,
		"DiscardCheckOutput": false,
		"DiscoveryMaxStale": "0s",
		"EnableAgentTLSForChecks": false,
		"EnableDebug": false,
		"EnableCentralServiceConfig": false,
		"EnableLocalScriptChecks": false,
		"EnableRemoteScriptChecks": false,
		"EnableSyslog": false,
		"EnableUI": false,
		"EncryptKey": "hidden",
		"EncryptVerifyIncoming": false,
		"EncryptVerifyOutgoing": false,
		"GRPCAddrs": [],
		"GRPCPort": 0,
		"HTTPAddrs": [
			"tcp://1.2.3.4:5678",
			"unix:///var/run/foo"
		],
		"HTTPBlockEndpoints": [],
		"HTTPPort": 0,
		"HTTPResponseHeaders": {},
		"HTTPSAddrs": [],
		"HTTPSPort": 0,
		"KeyFile": "hidden",
		"LeaveDrainTime": "0s",
		"LeaveOnTerm": false,
		"LogLevel": "",
		"LogFile": "",
		"LogRotateBytes": 0,
		"LogRotateDuration": "0s",
		"NodeID": "",
		"NodeMeta": {},
		"NodeName": "",
		"NonVotingServer": false,
		"PidFile": "",
		"PrimaryDatacenter": "",
		"RPCAdvertiseAddr": "",
		"RPCBindAddr": "",
		"RPCHoldTimeout": "0s",
		"RPCMaxBurst": 0,
		"RPCProtocol": 0,
		"RPCRateLimit": 0,
		"RaftProtocol": 0,
		"RaftSnapshotInterval": "0s",
		"RaftSnapshotThreshold": 0,
		"ReconnectTimeoutLAN": "0s",
		"ReconnectTimeoutWAN": "0s",
		"RejoinAfterLeave": false,
		"RetryJoinIntervalLAN": "0s",
		"RetryJoinIntervalWAN": "0s",
		"RetryJoinLAN": [
			"foo=bar key=hidden secret=hidden bang=bar"
		],
		"RetryJoinMaxAttemptsLAN": 0,
		"RetryJoinMaxAttemptsWAN": 0,
		"RetryJoinWAN": [
			"wan_foo=bar wan_key=hidden wan_secret=hidden wan_bang=bar"
		],
		"Revision": "",
		"SegmentLimit": 0,
		"SegmentName": "",
		"SegmentNameLimit": 0,
		"Segments": [],
		"SerfAdvertiseAddrLAN": "tcp://1.2.3.4:5678",
		"SerfAdvertiseAddrWAN": "",
		"SerfBindAddrLAN": "",
		"SerfBindAddrWAN": "",
		"SerfPortLAN": 0,
		"SerfPortWAN": 0,
		"ServerMode": false,
		"ServerName": "",
		"ServerPort": 0,
		"Services": [{
			"Address": "",
			"Check": {
				"AliasNode": "",
				"AliasService": "",
				"CheckID": "",
				"DeregisterCriticalServiceAfter": "0s",
				"DockerContainerID": "",
				"GRPC": "",
				"GRPCUseTLS": false,
				"HTTP": "",
				"Header": {},
				"Interval": "0s",
				"Method": "",
				"Name": "blurb",
				"Notes": "",
				"OutputMaxSize": ` + strconv.Itoa(checks.DefaultBufSize) + `,
				"ScriptArgs": [],
				"Shell": "",
				"Status": "",
				"TCP": "",
				"TLSSkipVerify": false,
				"TTL": "0s",
				"Timeout": "0s"
			},
			"Checks": [],
			"Connect": null,
			"EnableTagOverride": false,
			"ID": "",
			"Kind": "",
			"Meta": {},
			"Name": "foo",
			"Port": 0,
			"Proxy": null,
			"ProxyDestination": "",
			"Tags": [],
			"Token": "hidden",
			"Weights": {
				"Passing": 67,
				"Warning": 3
			}
		}],
		"SessionTTLMin": "0s",
		"SkipLeaveOnInt": false,
		"StartJoinAddrsLAN": [],
		"StartJoinAddrsWAN": [],
		"SyncCoordinateIntervalMin": "0s",
		"SyncCoordinateRateTarget": 0,
		"SyslogFacility": "",
		"TLSCipherSuites": [],
		"TLSMinVersion": "",
		"TLSPreferServerCipherSuites": false,
		"TaggedAddresses": {},
		"Telemetry": {
			"AllowedPrefixes": [],
			"BlockedPrefixes": [],
			"CirconusAPIApp": "",
			"CirconusAPIToken": "hidden",
			"CirconusAPIURL": "",
			"CirconusBrokerID": "",
			"CirconusBrokerSelectTag": "",
			"CirconusCheckDisplayName": "",
			"CirconusCheckForceMetricActivation": "",
			"CirconusCheckID": "",
			"CirconusCheckInstanceID": "",
			"CirconusCheckSearchTag": "",
			"CirconusCheckTags": "",
			"CirconusSubmissionInterval": "",
			"CirconusSubmissionURL": "",
			"DisableHostname": false,
			"DogstatsdAddr": "",
			"DogstatsdTags": [],
			"FilterDefault": false,
			"MetricsPrefix": "",
			"PrometheusRetentionTime": "0s",
			"StatsdAddr": "",
			"StatsiteAddr": ""
		},
		"TranslateWANAddrs": false,
		"UIContentPath": "",
		"UIDir": "",
		"UnixSocketGroup": "",
		"UnixSocketMode": "",
		"UnixSocketUser": "",
		"VerifyIncoming": false,
		"VerifyIncomingHTTPS": false,
		"VerifyIncomingRPC": false,
		"VerifyOutgoing": false,
		"VerifyServerHostname": false,
		"Version": "",
		"VersionPrerelease": "",
		"Watches": [],
		"AllowWriteHTTPFrom": []
	}`
	b, err := json.MarshalIndent(rt.Sanitized(), "", "    ")
	if err != nil {
		t.Fatal(err)
	}
	require.JSONEq(t, rtJSON, string(b))
}

func TestRuntime_apiAddresses(t *testing.T) {
	rt := RuntimeConfig{
		HTTPAddrs: []net.Addr{
			&net.TCPAddr{IP: net.ParseIP("198.18.0.1"), Port: 5678},
			&net.UnixAddr{Name: "/var/run/foo"},
		},
		HTTPSAddrs: []net.Addr{
			&net.TCPAddr{IP: net.ParseIP("198.18.0.2"), Port: 5678},
		}}

	unixAddrs, httpAddrs, httpsAddrs := rt.apiAddresses(1)

	require.Len(t, unixAddrs, 1)
	require.Len(t, httpAddrs, 1)
	require.Len(t, httpsAddrs, 1)

	require.Equal(t, "/var/run/foo", unixAddrs[0])
	require.Equal(t, "198.18.0.1:5678", httpAddrs[0])
	require.Equal(t, "198.18.0.2:5678", httpsAddrs[0])
}

func TestRuntime_APIConfigHTTPS(t *testing.T) {
	rt := RuntimeConfig{
		HTTPAddrs: []net.Addr{
			&net.TCPAddr{IP: net.ParseIP("198.18.0.1"), Port: 5678},
			&net.UnixAddr{Name: "/var/run/foo"},
		},
		HTTPSAddrs: []net.Addr{
			&net.TCPAddr{IP: net.ParseIP("198.18.0.2"), Port: 5678},
		},
		Datacenter:     "dc-test",
		CAFile:         "/etc/consul/ca.crt",
		CAPath:         "/etc/consul/ca.dir",
		CertFile:       "/etc/consul/server.crt",
		KeyFile:        "/etc/consul/ssl/server.key",
		VerifyOutgoing: false,
	}

	cfg, err := rt.APIConfig(false)
	require.NoError(t, err)
	require.Equal(t, "198.18.0.2:5678", cfg.Address)
	require.Equal(t, "https", cfg.Scheme)
	require.Equal(t, rt.CAFile, cfg.TLSConfig.CAFile)
	require.Equal(t, rt.CAPath, cfg.TLSConfig.CAPath)
	require.Equal(t, "", cfg.TLSConfig.CertFile)
	require.Equal(t, "", cfg.TLSConfig.KeyFile)
	require.Equal(t, rt.Datacenter, cfg.Datacenter)
	require.Equal(t, true, cfg.TLSConfig.InsecureSkipVerify)

	rt.VerifyOutgoing = true
	cfg, err = rt.APIConfig(true)
	require.NoError(t, err)
	require.Equal(t, "198.18.0.2:5678", cfg.Address)
	require.Equal(t, "https", cfg.Scheme)
	require.Equal(t, rt.CAFile, cfg.TLSConfig.CAFile)
	require.Equal(t, rt.CAPath, cfg.TLSConfig.CAPath)
	require.Equal(t, rt.CertFile, cfg.TLSConfig.CertFile)
	require.Equal(t, rt.KeyFile, cfg.TLSConfig.KeyFile)
	require.Equal(t, rt.Datacenter, cfg.Datacenter)
	require.Equal(t, false, cfg.TLSConfig.InsecureSkipVerify)
}

func TestRuntime_APIConfigHTTP(t *testing.T) {
	rt := RuntimeConfig{
		HTTPAddrs: []net.Addr{
			&net.UnixAddr{Name: "/var/run/foo"},
			&net.TCPAddr{IP: net.ParseIP("198.18.0.1"), Port: 5678},
		},
		Datacenter: "dc-test",
	}

	cfg, err := rt.APIConfig(false)
	require.NoError(t, err)
	require.Equal(t, rt.Datacenter, cfg.Datacenter)
	require.Equal(t, "198.18.0.1:5678", cfg.Address)
	require.Equal(t, "http", cfg.Scheme)
	require.Equal(t, "", cfg.TLSConfig.CAFile)
	require.Equal(t, "", cfg.TLSConfig.CAPath)
	require.Equal(t, "", cfg.TLSConfig.CertFile)
	require.Equal(t, "", cfg.TLSConfig.KeyFile)
}

func TestRuntime_APIConfigUNIX(t *testing.T) {
	rt := RuntimeConfig{
		HTTPAddrs: []net.Addr{
			&net.UnixAddr{Name: "/var/run/foo"},
		},
		Datacenter: "dc-test",
	}

	cfg, err := rt.APIConfig(false)
	require.NoError(t, err)
	require.Equal(t, rt.Datacenter, cfg.Datacenter)
	require.Equal(t, "unix:///var/run/foo", cfg.Address)
	require.Equal(t, "http", cfg.Scheme)
	require.Equal(t, "", cfg.TLSConfig.CAFile)
	require.Equal(t, "", cfg.TLSConfig.CAPath)
	require.Equal(t, "", cfg.TLSConfig.CertFile)
	require.Equal(t, "", cfg.TLSConfig.KeyFile)
}

func TestRuntime_APIConfigANYAddrV4(t *testing.T) {
	rt := RuntimeConfig{
		HTTPAddrs: []net.Addr{
			&net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Port: 5678},
		},
		Datacenter: "dc-test",
	}

	cfg, err := rt.APIConfig(false)
	require.NoError(t, err)
	require.Equal(t, rt.Datacenter, cfg.Datacenter)
	require.Equal(t, "127.0.0.1:5678", cfg.Address)
	require.Equal(t, "http", cfg.Scheme)
	require.Equal(t, "", cfg.TLSConfig.CAFile)
	require.Equal(t, "", cfg.TLSConfig.CAPath)
	require.Equal(t, "", cfg.TLSConfig.CertFile)
	require.Equal(t, "", cfg.TLSConfig.KeyFile)
}

func TestRuntime_APIConfigANYAddrV6(t *testing.T) {
	rt := RuntimeConfig{
		HTTPAddrs: []net.Addr{
			&net.TCPAddr{IP: net.ParseIP("::"), Port: 5678},
		},
		Datacenter: "dc-test",
	}

	cfg, err := rt.APIConfig(false)
	require.NoError(t, err)
	require.Equal(t, rt.Datacenter, cfg.Datacenter)
	require.Equal(t, "[::1]:5678", cfg.Address)
	require.Equal(t, "http", cfg.Scheme)
	require.Equal(t, "", cfg.TLSConfig.CAFile)
	require.Equal(t, "", cfg.TLSConfig.CAPath)
	require.Equal(t, "", cfg.TLSConfig.CertFile)
	require.Equal(t, "", cfg.TLSConfig.KeyFile)
}

func TestRuntime_ClientAddress(t *testing.T) {
	rt := RuntimeConfig{
		HTTPAddrs: []net.Addr{
			&net.TCPAddr{IP: net.ParseIP("::"), Port: 5678},
			&net.TCPAddr{IP: net.ParseIP("198.18.0.1"), Port: 5679},
			&net.UnixAddr{Name: "/var/run/foo", Net: "unix"},
		},
		HTTPSAddrs: []net.Addr{
			&net.TCPAddr{IP: net.ParseIP("::"), Port: 5688},
			&net.TCPAddr{IP: net.ParseIP("198.18.0.1"), Port: 5689},
		},
	}

	unix, http, https := rt.ClientAddress()

	require.Equal(t, "unix:///var/run/foo", unix)
	require.Equal(t, "198.18.0.1:5679", http)
	require.Equal(t, "198.18.0.1:5689", https)
}

func TestRuntime_ClientAddressAnyV4(t *testing.T) {
	rt := RuntimeConfig{
		HTTPAddrs: []net.Addr{
			&net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Port: 5678},
			&net.UnixAddr{Name: "/var/run/foo", Net: "unix"},
		},
		HTTPSAddrs: []net.Addr{
			&net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Port: 5688},
		},
	}

	unix, http, https := rt.ClientAddress()

	require.Equal(t, "unix:///var/run/foo", unix)
	require.Equal(t, "127.0.0.1:5678", http)
	require.Equal(t, "127.0.0.1:5688", https)
}

func TestRuntime_ClientAddressAnyV6(t *testing.T) {
	rt := RuntimeConfig{
		HTTPAddrs: []net.Addr{
			&net.TCPAddr{IP: net.ParseIP("::"), Port: 5678},
			&net.UnixAddr{Name: "/var/run/foo", Net: "unix"},
		},
		HTTPSAddrs: []net.Addr{
			&net.TCPAddr{IP: net.ParseIP("::"), Port: 5688},
		},
	}

	unix, http, https := rt.ClientAddress()

	require.Equal(t, "unix:///var/run/foo", unix)
	require.Equal(t, "[::1]:5678", http)
	require.Equal(t, "[::1]:5688", https)
}

func TestRuntime_ToTLSUtilConfig(t *testing.T) {
	c := &RuntimeConfig{
		VerifyIncoming:              true,
		VerifyIncomingRPC:           true,
		VerifyIncomingHTTPS:         true,
		VerifyOutgoing:              true,
		VerifyServerHostname:        true,
		CAFile:                      "a",
		CAPath:                      "b",
		CertFile:                    "c",
		KeyFile:                     "d",
		NodeName:                    "e",
		ServerName:                  "f",
		DNSDomain:                   "g",
		TLSMinVersion:               "tls12",
		TLSCipherSuites:             []uint16{tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305},
		TLSPreferServerCipherSuites: true,
		EnableAgentTLSForChecks:     true,
	}
	r := c.ToTLSUtilConfig()
	require.Equal(t, c.VerifyIncoming, r.VerifyIncoming)
	require.Equal(t, c.VerifyIncomingRPC, r.VerifyIncomingRPC)
	require.Equal(t, c.VerifyIncomingHTTPS, r.VerifyIncomingHTTPS)
	require.Equal(t, c.VerifyOutgoing, r.VerifyOutgoing)
	require.Equal(t, c.VerifyServerHostname, r.VerifyServerHostname)
	require.Equal(t, c.CAFile, r.CAFile)
	require.Equal(t, c.CAPath, r.CAPath)
	require.Equal(t, c.CertFile, r.CertFile)
	require.Equal(t, c.KeyFile, r.KeyFile)
	require.Equal(t, c.NodeName, r.NodeName)
	require.Equal(t, c.ServerName, r.ServerName)
	require.Equal(t, c.DNSDomain, r.Domain)
	require.Equal(t, c.TLSMinVersion, r.TLSMinVersion)
	require.Equal(t, c.TLSCipherSuites, r.CipherSuites)
	require.Equal(t, c.TLSPreferServerCipherSuites, r.PreferServerCipherSuites)
	require.Equal(t, c.EnableAgentTLSForChecks, r.EnableAgentTLSForChecks)
}

func TestReadPath(t *testing.T) {
	dataDir := testutil.TempDir(t, "consul")
	defer os.RemoveAll(dataDir)

	tt := []struct {
		name   string
		pre    func()
		args   []string
		expect int
	}{
		{
			name: "dir skip non json or hcl if config-format not set",
			pre: func() {
				writeFile(filepath.Join(dataDir, "conf.d/conf.json"), []byte(`{}`))
				writeFile(filepath.Join(dataDir, "conf.d/conf.foobar"), []byte(`{}`))
			},
			args: []string{
				`-config-dir`, filepath.Join(dataDir, "conf.d"),
			},
			expect: 1,
		},
		{
			name: "dir read non json or hcl if config-format set",
			pre: func() {
				writeFile(filepath.Join(dataDir, "conf.d/conf.json"), []byte(`{}`))
				writeFile(filepath.Join(dataDir, "conf.d/conf.foobar"), []byte(`{}`))
			},
			args: []string{
				`-config-dir`, filepath.Join(dataDir, "conf.d"),
				`-config-format`, "json",
			},
			expect: 2,
		},
		{
			name: "file skip non json or hcl if config-format not set",
			pre: func() {
				writeFile(filepath.Join(dataDir, "conf.d/conf.foobar"), []byte(`{}`))
			},
			args: []string{
				`-config-file`, filepath.Join(dataDir, "conf.d"),
			},
			expect: 0,
		},
		{
			name: "file read non json or hcl if config-format set",
			pre: func() {
				writeFile(filepath.Join(dataDir, "conf.d/conf.foobar"), []byte(`{}`))
			},
			args: []string{
				`-config-file`, filepath.Join(dataDir, "conf.d"),
				`-config-format`, "json",
			},
			expect: 1,
		},
	}

	for _, tc := range tt {
		cleanDir(dataDir)

		t.Run(tc.name, func(t *testing.T) {
			flags := Flags{}
			fs := flag.NewFlagSet("", flag.ContinueOnError)
			AddFlags(fs, &flags)
			err := fs.Parse(tc.args)
			if err != nil {
				t.Fatalf("ParseFlags failed: %s", err)
			}
			flags.Args = fs.Args()

			// write cfg files
			tc.pre()

			// Then create a builder with the flags.
			b, err := NewBuilder(flags)
			if err != nil {
				t.Fatal("NewBuilder", err)
			}

			got := len(b.Sources)
			if tc.expect != got {
				t.Fatalf("expected %d sources, got %d", tc.expect, got)
			}
		})
	}
}

func Test_UIPathBuilder(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		expected string
	}{
		{
			"Letters only string",
			"hello",
			"/hello/",
		},
		{
			"Alphanumeric",
			"Hello1",
			"/Hello1/",
		},
		{
			"Hyphen and underscore",
			"-_",
			"/-_/",
		},
		{
			"Many slashes",
			"/hello/ui/1/",
			"/hello/ui/1/",
		},
	}

	for _, tt := range cases {
		actual := UIPathBuilder(tt.path)
		require.Equal(t, tt.expected, actual)

	}
}

func splitIPPort(hostport string) (net.IP, int) {
	h, p, err := net.SplitHostPort(hostport)
	if err != nil {
		panic(err)
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		panic(err)
	}
	return net.ParseIP(h), port
}

func ipAddr(addr string) *net.IPAddr {
	return &net.IPAddr{IP: net.ParseIP(addr)}
}

func tcpAddr(addr string) *net.TCPAddr {
	ip, port := splitIPPort(addr)
	return &net.TCPAddr{IP: ip, Port: port}
}

func udpAddr(addr string) *net.UDPAddr {
	ip, port := splitIPPort(addr)
	return &net.UDPAddr{IP: ip, Port: port}
}

func unixAddr(addr string) *net.UnixAddr {
	if !strings.HasPrefix(addr, "unix://") {
		panic("not a unix socket addr: " + addr)
	}
	return &net.UnixAddr{Net: "unix", Name: addr[len("unix://"):]}
}

func writeFile(path string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		panic(err)
	}
	if err := ioutil.WriteFile(path, data, 0640); err != nil {
		panic(err)
	}
}

func cleanDir(path string) {
	root := path
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if path == root {
			return nil
		}
		return os.RemoveAll(path)
	})
	if err != nil {
		panic(err)
	}
}

func randomString(n int) string {
	s := ""
	for ; n > 0; n-- {
		s += "x"
	}
	return s
}

func metaPairs(n int, format string) string {
	var s []string
	for i := 0; i < n; i++ {
		switch format {
		case "json":
			s = append(s, fmt.Sprintf(`"%d":"%d"`, i, i))
		case "hcl":
			s = append(s, fmt.Sprintf(`"%d"="%d"`, i, i))
		default:
			panic("invalid format: " + format)
		}
	}
	switch format {
	case "json":
		return strings.Join(s, ",")
	case "hcl":
		return strings.Join(s, " ")
	default:
		panic("invalid format: " + format)
	}
}
