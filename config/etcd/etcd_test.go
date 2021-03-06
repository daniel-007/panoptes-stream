//: Copyright Verizon Media
//: Licensed under the terms of the Apache 2.0 License. See LICENSE file in the project root for terms.

package etcd

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yahoo/panoptes-stream/config"
	"go.etcd.io/etcd/clientv3"
	"go.etcd.io/etcd/integration"
)

func TestNewEtcd(t *testing.T) {
	cluster := integration.NewClusterV3(t, &integration.ClusterConfig{Size: 1})
	defer cluster.Terminate(t)

	client := cluster.RandClient()
	os.Setenv("PANOPTES_CONFIG_ETCD_ENDPOINTS", client.Endpoints()[0])
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	kv := clientv3.NewKV(client)
	kv.Put(ctx, "panoptes/config/devices/core1.lax", `{"host": "core1.bur","port": 50051,"sensors" : ["sensor1"]}`)
	kv.Put(ctx, "panoptes/config/sensors/sensor1", `{"service": "arista.gnmi","output":"console::stdout", "path": "/interfaces/", "mode": "sample"}`)
	kv.Put(ctx, "panoptes/config/databases/db1", `{"service": "influxdb", "config": {"server": "https://localhost:8086"}}`)
	kv.Put(ctx, "panoptes/config/producers/kafka1", `{"service": "kafka", "config" : {"brokers": ["127.0.0.1:9092"], "topics":["bgp"]}}`)
	kv.Put(ctx, "panoptes/config/global", `{"logger": {"level":"info", "encoding": "console", "outputPaths": ["stdout"], "errorOutputPaths":["stderr"]}, "status": {"addr":"127.0.0.2:8081"}}`)

	cfg, err := New("-")
	assert.Equal(t, nil, err)

	devices := cfg.Devices()
	databases := cfg.Databases()
	producers := cfg.Producers()
	sensors := cfg.Sensors()

	assert.Len(t, devices, 1)
	assert.Len(t, databases, 1)
	assert.Len(t, producers, 2)
	assert.Len(t, sensors, 1)

	assert.Equal(t, "core1.bur", devices[0].Host)
	assert.Equal(t, "influxdb", databases[0].Service)
	assert.Equal(t, "kafka", producers[0].Service)
	assert.Equal(t, "arista.gnmi", sensors[0].Service)
	assert.NotNil(t, cfg.Informer())
	assert.Equal(t, "127.0.0.2:8081", cfg.Global().Status.Addr)
	assert.NotEqual(t, nil, cfg.Logger())

	// make sure watch is ready
	time.Sleep(time.Second)

	kv.Put(ctx, "panoptes/config/databases/db2", `{"service": "influxdb", "config": {"server": "https://localhost:8086"}}`)

	select {
	case <-cfg.Informer():
	case <-ctx.Done():
		assert.Fail(t, "context deadline exceeded")
	}

	cfg.Update()
	databases = cfg.Databases()
	assert.Len(t, databases, 2)

	// invalid json data
	kv.Put(ctx, "panoptes/config/devices/core1.lax", `"host": "core1.bur","port": 50051,"sensors" : ["sensor1"]}`)
	err = cfg.Update()
	assert.NotEqual(t, nil, err)
}

func TestEmptyConfig(t *testing.T) {
	cluster := integration.NewClusterV3(t, &integration.ClusterConfig{Size: 1})
	defer cluster.Terminate(t)

	client := cluster.RandClient()
	os.Setenv("PANOPTES_CONFIG_ETCD_ENDPOINTS", client.Endpoints()[0])
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	kv := clientv3.NewKV(client)
	kv.Put(ctx, "config/", "")

	_, err := New("-")
	assert.Error(t, err)
}

func TestSignalHandler(t *testing.T) {
	ch := make(chan struct{}, 1)
	cfg := config.NewMockConfig()
	e := &etcd{
		informer: ch,
		logger:   cfg.Logger(),
	}

	go e.signalHandler()
	time.Sleep(time.Second)

	proc, err := os.FindProcess(os.Getpid())
	assert.NoError(t, err)
	proc.Signal(syscall.SIGHUP)

	select {
	case <-ch:
	case <-time.After(time.Second):
		assert.Fail(t, "time exceeded")
	}

	proc.Signal(syscall.SIGHUP)
	time.Sleep(100 * time.Millisecond)
	proc.Signal(syscall.SIGHUP)

	time.Sleep(time.Second)
	assert.Contains(t, cfg.LogOutput.String(), "dropped")
}
