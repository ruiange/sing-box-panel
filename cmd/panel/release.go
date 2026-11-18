package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/ruiange/sing-box-panel/internal/panel"
)

const (
	releaseRepo = "ruiange/sing-box-panel"
	latestURL   = "https://api.github.com/repos/" + releaseRepo + "/releases/latest"
	usage       = "用法：sing-box-panel [version | latest | download [目录] | validate-address 公网IP | domain-url 数据库路径]"
)

// Set by deploy/package.sh with -ldflags "-X main.version=<tag>".
var version = "dev"

type releaseInfo struct {
	Tag        string         `json:"tag_name"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

func runCommand(args []string) int {
	switch args[0] {
	case "help", "--help", "-h":
		if len(args) == 1 {
			fmt.Fprintln(os.Stdout, usage)
			return 0
		}
	case "validate-address":
		if len(args) == 2 {
			address, err := panel.PublicIP(args[1])
			if err != nil {
				return reportCommandError(err, 2)
			}
			fmt.Fprintln(os.Stdout, address)
			return 0
		}
	case "domain-url":
		if len(args) == 2 && args[1] != "" {
			address, err := panel.ConfiguredDomainURL(args[1])
			if err != nil {
				return reportCommandError(err, 1)
			}
			if address != "" {
				fmt.Fprintln(os.Stdout, address)
			}
			return 0
		}
	case "version", "--version", "-v":
		if len(args) == 1 {
			fmt.Fprintln(os.Stdout, version)
			return 0
		}
	case "latest":
		if len(args) == 1 {
			return runReleaseCommand("")
		}
	case "download":
		if len(args) == 1 {
			return runReleaseCommand(".")
		}
		if len(args) == 2 && args[1] != "" {
			return runReleaseCommand(args[1])
		}
	}
	return reportCommandError(errors.New(usage), 2)
}

func reportCommandError(err error, exitCode int) int {
	fmt.Fprintln(os.Stderr, err)
	return exitCode
}

func runReleaseCommand(directory string) int {
	if directory != "" && (runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64")) {
		return reportCommandError(fmt.Errorf("当前平台 %s/%s 没有安装包", runtime.GOOS, runtime.GOARCH), 1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	client := &http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || len(via) >= 10 {
				return errors.New("GitHub 下载重定向无效")
			}
			return nil
		},
	}
	latest, err := getLatestRelease(ctx, client)
	if err != nil {
		return reportCommandError(err, 1)
	}
	if directory == "" {
		fmt.Fprintln(os.Stdout, latest.Tag)
		return 0
	}
	path, err := downloadRelease(ctx, client, latest, runtime.GOARCH, directory)
	if err != nil {
		return reportCommandError(err, 1)
	}
	fmt.Fprintf(os.Stdout, "%s 下载完成，SHA-256 校验通过\n%s\n", latest.Tag, path)
	return 0
}

func getLatestRelease(ctx context.Context, client *http.Client) (releaseInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return releaseInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	response, err := client.Do(req)
	if err != nil {
		return releaseInfo{}, fmt.Errorf("查询 GitHub 最新版本失败：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return releaseInfo{}, fmt.Errorf("查询 GitHub 最新版本失败（HTTP %d）", response.StatusCode)
	}

	var latest releaseInfo
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&latest); err != nil {
		return releaseInfo{}, fmt.Errorf("读取 GitHub 发布信息失败：%w", err)
	}
	if latest.Tag == "" || latest.Draft || latest.Prerelease {
		return releaseInfo{}, errors.New("GitHub 没有返回正式发布版本")
	}
	return latest, nil
}

func downloadRelease(ctx context.Context, client *http.Client, release releaseInfo, arch, directory string) (string, error) {
	name := "sing-box-panel-linux-" + arch + ".tar.gz"
	var asset releaseAsset
	for _, candidate := range release.Assets {
		if candidate.Name == name && candidate.State == "uploaded" {
			asset = candidate
			break
		}
	}
	if asset.Name == "" {
		return "", fmt.Errorf("版本 %s 缺少安装包 %s", release.Tag, name)
	}
	digest, err := hex.DecodeString(strings.TrimPrefix(asset.Digest, "sha256:"))
	if !strings.HasPrefix(asset.Digest, "sha256:") || err != nil || len(digest) != sha256.Size ||
		asset.Size <= 0 || asset.Size == math.MaxInt64 {
		return "", fmt.Errorf("版本 %s 缺少安装包的有效 SHA-256 或文件大小", release.Tag)
	}

	directory, err = filepath.Abs(directory)
	if err != nil {
		return "", fmt.Errorf("解析下载目录失败：%w", err)
	}
	target := filepath.Join(directory, name)
	if _, err := os.Lstat(target); err == nil {
		return "", fmt.Errorf("目标文件已存在：%s", target)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("检查目标文件失败：%w", err)
	}
	tmp, err := os.CreateTemp(directory, "."+name+".*.part")
	if err != nil {
		return "", fmt.Errorf("创建下载临时文件失败：%w", err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	downloadURL := "https://github.com/" + releaseRepo + "/releases/download/" + url.PathEscape(release.Tag) + "/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("下载安装包失败：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载安装包失败（HTTP %d）", response.StatusCode)
	}
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(response.Body, asset.Size+1))
	if err != nil {
		return "", fmt.Errorf("写入安装包失败：%w", err)
	}
	if size != asset.Size || !bytes.Equal(hash.Sum(nil), digest) {
		return "", errors.New("安装包大小或 SHA-256 校验失败")
	}
	if err := tmp.Sync(); err != nil {
		return "", fmt.Errorf("同步安装包失败：%w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("关闭安装包失败：%w", err)
	}
	// Linking makes the verified file visible atomically and never replaces an existing file.
	if err := os.Link(tmp.Name(), target); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("目标文件已存在：%s", target)
		}
		return "", fmt.Errorf("保存安装包失败：%w", err)
	}
	return target, nil
}
