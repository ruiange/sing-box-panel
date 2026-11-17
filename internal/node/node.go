package node

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"google.golang.org/grpc"
)

const (
	commandTimeout = 15 * time.Second
	healthTimeout  = 8 * time.Second
)

type Node struct {
	cfg  Config
	conn *grpc.ClientConn
	mu   sync.Mutex
}

func New(cfg Config) (*Node, error) {
	if err := cfg.prepare(); err != nil {
		return nil, err
	}
	if cfg.RecordDestinations {
		if err := cfg.prepareDestinationAPI(); err != nil {
			return nil, err
		}
	}
	conn, err := newStatsConnection(cfg.StatsAddress)
	if err != nil {
		return nil, err
	}
	return &Node{cfg: cfg, conn: conn}, nil
}

func (n *Node) Close() error {
	return n.conn.Close()
}

func (n *Node) Collect(ctx context.Context) (Snapshot, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.collect(ctx)
}

// Apply validates the new configuration in the same directory, replaces the
// live file atomically, and verifies that a fresh statistics service responds.
// It reports whether a restart was confirmed, including a successful recovery.
func (n *Node) Apply(ctx context.Context, devices []Device) (reloaded bool, err error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	payload, err := renderConfig(n.cfg, devices)
	if err != nil {
		return false, err
	}
	old, oldMode, err := readConfig(n.cfg.ConfigPath)
	if err != nil {
		return false, err
	}
	previous, probeErr := n.probe(ctx)
	if bytes.Equal(old, payload) && probeErr == nil {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}

	tempPath, err := prepareFile(n.cfg.ConfigPath, payload, 0600)
	if err != nil {
		return false, err
	}
	defer os.Remove(tempPath)
	if err := runCommand(ctx, n.cfg.BinaryPath, "check", "-c", tempPath); err != nil {
		return false, fmt.Errorf("sing-box 配置校验失败: %w", err)
	}

	// Keep at least one uptime tick between a newly started old service and
	// its replacement so their start windows do not overlap after a reload.
	if err := waitUntil(ctx, previous.latest.Add(1250*time.Millisecond)); err != nil {
		return false, err
	}
	if err := os.Rename(tempPath, n.cfg.ConfigPath); err != nil {
		return false, fmt.Errorf("写入节点配置: %w", err)
	}
	if err := syncContainingDir(n.cfg.ConfigPath); err != nil {
		return false, errors.Join(fmt.Errorf("同步节点配置: %w", err), restoreFile(n.cfg.ConfigPath, old, oldMode))
	}
	if err := n.reload(ctx, previous.latest); err != nil {
		recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), commandTimeout+healthTimeout+3*time.Second)
		defer cancel()
		reloadErr := fmt.Errorf("sing-box 应用配置失败: %w", err)
		if err := restoreFile(n.cfg.ConfigPath, old, oldMode); err != nil {
			return false, errors.Join(reloadErr, fmt.Errorf("恢复 sing-box 配置失败: %w", err))
		}
		if old == nil {
			return false, reloadErr
		}
		current, _ := n.probe(recovery)
		if err := waitUntil(recovery, current.latest.Add(1250*time.Millisecond)); err != nil {
			return false, errors.Join(reloadErr, err)
		}
		if err := n.reload(recovery, current.latest); err != nil {
			return false, errors.Join(reloadErr, fmt.Errorf("重新启用旧配置失败: %w", err))
		}
		return true, reloadErr
	}
	return true, nil
}

func (n *Node) probe(ctx context.Context) (startWindow, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	return n.getStartWindow(ctx)
}

func (n *Node) reload(ctx context.Context, previousLatest time.Time) error {
	command := n.cfg.ReloadCommand
	beforeReload := time.Now()
	if err := runCommand(ctx, command[0], command[1:]...); err != nil {
		return err
	}
	healthCtx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	for {
		current, err := n.probe(healthCtx)
		if err == nil && current.earliest.After(previousLatest) && !current.latest.Before(beforeReload) {
			return nil
		}
		if err := waitUntil(healthCtx, time.Now().Add(200*time.Millisecond)); err != nil {
			return fmt.Errorf("新的 sing-box 统计服务未就绪: %w", err)
		}
	}
}

func runCommand(ctx context.Context, executable string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	// Only the operator config and the generated temp-file path become
	// arguments. Never expose potentially secret process output in an API
	// error.
	command := exec.CommandContext(ctx, executable, args...)
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	return nil
}

func waitUntil(ctx context.Context, then time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if delay := time.Until(then); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func readConfig(path string) ([]byte, fs.FileMode, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("读取现有节点配置: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, 0, errors.New("节点配置必须为普通文件")
	}
	if info.Mode().Perm()&0077 != 0 {
		return nil, 0, errors.New("节点配置文件包含密钥，须限制为所有者可访问")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, fmt.Errorf("读取现有节点配置: %w", err)
	}
	return data, info.Mode().Perm(), nil
}

func prepareFile(path string, payload []byte, mode fs.FileMode) (string, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("创建节点配置目录: %w", err)
	}
	file, err := os.CreateTemp(dir, ".sing-box-panel-*.json")
	if err != nil {
		return "", fmt.Errorf("创建节点临时配置: %w", err)
	}
	name := file.Name()
	if err := writeConfig(file, payload, mode); err != nil {
		_ = os.Remove(name)
		return "", fmt.Errorf("写入节点临时配置: %w", err)
	}
	return name, nil
}

func writeConfig(file *os.File, payload []byte, mode fs.FileMode) (err error) {
	defer func() {
		err = errors.Join(err, file.Close())
	}()
	if err := file.Chmod(mode); err != nil {
		return err
	}
	if _, err := file.Write(payload); err != nil {
		return err
	}
	return file.Sync()
}

func restoreFile(path string, old []byte, oldMode fs.FileMode) error {
	if old == nil {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return syncContainingDir(path)
	}
	tempPath, err := prepareFile(path, old, oldMode)
	if err != nil {
		return err
	}
	defer os.Remove(tempPath)
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return syncContainingDir(path)
}

func syncContainingDir(path string) error {
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
