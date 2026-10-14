package ciscoiosxe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type saveTransport struct {
	calls   int
	failure error
	result  string
}

func (*saveTransport) Get(context.Context, string) ([]byte, error)       { return nil, nil }
func (*saveTransport) GetConfig(context.Context, string) ([]byte, error) { return nil, nil }
func (*saveTransport) Edit(context.Context, string) error                { return nil }
func (*saveTransport) Close() error                                      { return nil }
func (t *saveTransport) RPC(_ context.Context, operation, reply any) error {
	t.calls++
	copy, ok := operation.(copyStartup)
	if !ok || copy.Source != "running-config" || copy.Destination != "startup-config" {
		return errors.New("unexpected save request")
	}
	reply.(*copyStartupReply).Result = "RPC request successful"
	if t.result != "" {
		reply.(*copyStartupReply).Result = t.result
	}
	return t.failure
}

func TestNegativeSaveAcknowledgementKeepsPendingWork(t *testing.T) {
	collector := NewCollector(&saveTransport{result: "RPC request unsuccessful"})
	collector.saveConfig, collector.savePending = true, true
	if err := collector.saveStartup(context.Background()); err == nil || !collector.savePending {
		t.Fatal("negative save acknowledgement cleared pending work")
	}
}

func TestSaveRetriesAndPersistsPendingWork(t *testing.T) {
	transport := &saveTransport{failure: errors.New("save failed")}
	collector := NewCollector(transport)
	collector.saveConfig, collector.savePending = true, true
	collector.savePath = filepath.Join(t.TempDir(), "state", "startup-pending")
	if err := collector.saveStartup(context.Background()); err == nil {
		t.Fatal("save failure ignored")
	}
	if _, err := os.Stat(collector.savePath); err != nil || !collector.savePending {
		t.Fatal("pending save was lost")
	}
	transport.failure = nil
	if err := collector.saveStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(collector.savePath); !errors.Is(err, os.ErrNotExist) || collector.savePending {
		t.Fatal("completed save still pending")
	}
	if err := collector.saveStartup(context.Background()); err != nil || transport.calls != 2 {
		t.Fatal("unchanged state saved again")
	}
}
