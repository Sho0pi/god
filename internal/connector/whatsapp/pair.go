package whatsapp

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	_ "modernc.org/sqlite"
)

// pairConnectTimeout bounds the wait for the authenticated reconnection that
// follows a successful QR scan.
const pairConnectTimeout = 40 * time.Second

// Pair runs a standalone WhatsApp device login against the session store at
// storePath, outside the full connector lifecycle. It is used by the setup
// wizard.
//
//   - If a device is already registered and reset is false, it returns
//     ErrAlreadyPaired without touching anything.
//   - If reset is true, the existing store.db is deleted first, forcing a fresh
//     QR login.
//   - Otherwise it opens a QR channel, emits each code via qrFn, and blocks until
//     the phone links (success), ctx is cancelled, or the QR times out.
//
// The caller is responsible for ensuring no gateway is using the same store
// concurrently (the sqlite store allows a single connection).
func Pair(ctx context.Context, storePath string, reset bool, qrFn func(code string)) error {
	if reset {
		if err := os.Remove(filepath.Join(storePath, dbName)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("reset session: %w", err)
		}
	}
	if err := os.MkdirAll(storePath, 0o700); err != nil {
		return fmt.Errorf("create store dir: %w", err)
	}

	dbPath := filepath.Join(storePath, dbName)
	db, err := sql.Open(sqliteDriver, "file:"+dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("pragma: %w", err)
	}

	waLogger := waLog.Noop
	container := sqlstore.NewWithDB(db, sqliteDriver, waLogger)
	if err := container.Upgrade(ctx); err != nil {
		return fmt.Errorf("upgrade db: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("get device: %w", err)
	}

	client := whatsmeow.NewClient(deviceStore, waLogger)
	defer client.Disconnect()

	if client.Store.ID != nil {
		if !reset {
			return ErrAlreadyPaired
		}
	}

	// After the QR is scanned, whatsmeow finishes pairing, then drops the socket
	// and reconnects authenticated — only then is the session fully persisted.
	// We must wait for events.Connected before disconnecting, otherwise pairing
	// is left half-finished and the phone hangs on "login pending".
	connected := make(chan struct{}, 1)
	pairErr := make(chan error, 1)
	client.AddEventHandler(func(evt any) {
		switch e := evt.(type) {
		case *events.Connected:
			select {
			case connected <- struct{}{}:
			default:
			}
		case *events.PairError:
			select {
			case pairErr <- e.Error:
			default:
			}
		}
	})

	qrChan, err := client.GetQRChannel(ctx)
	if err != nil {
		return fmt.Errorf("get qr channel: %w", err)
	}
	if err := client.Connect(); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-pairErr:
			return fmt.Errorf("pairing failed: %w", err)
		case evt, open := <-qrChan:
			if !open {
				return fmt.Errorf("qr channel closed before login")
			}
			switch evt.Event {
			case "code":
				if qrFn != nil {
					qrFn(evt.Code)
				}
			case "success":
				// QR scanned. Wait for the authenticated reconnection so the
				// session is saved before we disconnect.
				return waitConnected(ctx, connected, pairErr)
			case "timeout":
				return fmt.Errorf("QR code timed out — try again")
			default:
				// other progress events ("error", etc.) — keep waiting
			}
		}
	}
}

// waitConnected blocks until the post-pair authenticated connection lands, the
// pairing errors, ctx is cancelled, or the timeout elapses.
func waitConnected(ctx context.Context, connected <-chan struct{}, pairErr <-chan error) error {
	select {
	case <-connected:
		return nil
	case err := <-pairErr:
		return fmt.Errorf("pairing failed: %w", err)
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(pairConnectTimeout):
		return fmt.Errorf("paired but did not finish connecting in time — try again")
	}
}

// ErrAlreadyPaired is returned by Pair when a session already exists and reset
// was not requested.
var ErrAlreadyPaired = fmt.Errorf("whatsapp: device already paired")

// SessionExists reports whether a WhatsApp session store (store.db) exists at
// storePath.
func SessionExists(storePath string) bool {
	_, err := os.Stat(filepath.Join(storePath, dbName))
	return err == nil
}
