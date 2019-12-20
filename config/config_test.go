package config

import (
	"os"
	"testing"

	"github.com/sprawl/sprawl/interfaces"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

const defaultConfigPath string = "default"
const invalidConfigPath string = "invalid"
const defaultDBPath string = "/var/lib/sprawl/data"
const defaultAPIPort string = "1337"
const defaultExternalIP string = ""
const defaultP2PPort string = "4001"
const defaultNATPortMapSetting bool = true
const defaultRelaySetting bool = true
const defaultAutoRelaySetting bool = true
const defaultDebugSetting bool = false
const defaultStackTraceSetting bool = false
const defaultLogLevel string = "INFO"
const defaultLogFormat string = "console"

const dbPathEnvVar string = "SPRAWL_DATABASE_PATH"
const rpcPortEnvVar string = "SPRAWL_RPC_PORT"
const p2pDebugEnvVar string = "SPRAWL_P2P_DEBUG"
const errorsEnableStackTraceEnvVar string = "SPRAWL_ERRORS_ENABLESTACKTRACE"

const envTestDBPath string = "/var/lib/sprawl/justforthistest"
const envTestAPIPort string = "9001"
const envTestP2PDebug string = "true"
const envTestErrorsEnableStackTrace string = "true"

var logger *zap.Logger
var log *zap.SugaredLogger
var config interfaces.Config

func init() {
	config = &Config{}
}

func resetEnv() {
	os.Unsetenv(dbPathEnvVar)
	os.Unsetenv(rpcPortEnvVar)
	os.Unsetenv(p2pDebugEnvVar)
	os.Unsetenv(errorsEnableStackTraceEnvVar)
}

func TestErrors(t *testing.T) {
	resetEnv()
	var dbPath string
	// Tests for panics when not initialized with a config file
	assert.Panics(t, func() { dbPath = config.GetDatabasePath() }, "Config should panic when no config file or environment variables are provided")
	assert.Equal(t, dbPath, "")
	// Test an invalid config file
	config.ReadConfig(invalidConfigPath)
	dbPath = config.GetDatabasePath()
	assert.Equal(t, dbPath, "")
}

func TestDefaults(t *testing.T) {
	resetEnv()
	config.ReadConfig(defaultConfigPath)

	databasePath := config.GetDatabasePath()
	rpcPort := config.GetRPCPort()
	p2pDebug := config.GetDebugSetting()
	errorsEnableStackTrace := config.GetStackTraceSetting()
	externalIP := config.GetExternalIP()
	p2pPort := config.GetP2PPort()
	NATPortMap := config.GetNATPortMapSetting()
	relay := config.GetRelaySetting()
	autoRelay := config.GetAutoRelaySetting()
	logLevel := config.GetLogLevel()
	logFormat := config.GetLogFormat()

	assert.Equal(t, databasePath, defaultDBPath)
	assert.Equal(t, rpcPort, defaultAPIPort)
	assert.Equal(t, p2pDebug, defaultDebugSetting)
	assert.Equal(t, errorsEnableStackTrace, defaultStackTraceSetting)
	assert.Equal(t, externalIP, defaultExternalIP)
	assert.Equal(t, p2pPort, defaultP2PPort)
	assert.Equal(t, NATPortMap, defaultNATPortMapSetting)
	assert.Equal(t, relay, defaultRelaySetting)
	assert.Equal(t, autoRelay, defaultAutoRelaySetting)
	assert.Equal(t, logLevel, defaultLogLevel)
	assert.Equal(t, logFormat, defaultLogFormat)
}

// TestEnvironment tests that environment variables overwrite any other configuration
func TestEnvironment(t *testing.T) {
	os.Setenv(dbPathEnvVar, envTestDBPath)

	config.ReadConfig("")
	databasePath := config.GetDatabasePath()

	assert.Equal(t, databasePath, envTestDBPath)

	resetEnv()
}
