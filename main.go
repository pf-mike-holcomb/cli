package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/franela/goreq"
	"github.com/ulikunitz/xz"
)

type Manifest struct {
	ReleasedAt string            `json:"released_at"`
	Version    string            `json:"version"`
	Channel    string            `json:"channel"`
	Builds     map[string]*Build `json:"builds"`
}
type Build struct {
	URL    string `json:"url"`
	Sha1   string `json:"sha1"`
	Sha256 string `json:"sha256"`
}

var DataHome = dataHome()
var HomeDir = homeDir()

func main() {
	loadNewCLI()
	update()
	loadNewCLI()
}

func localAppData() string {
	return os.Getenv("LOCALAPPDATA")
}

func homeDir() string {
	home := os.Getenv("HOME")
	if home != "" {
		return home
	}
	user, err := user.Current()
	if err != nil {
		panic(err)
	}
	return user.HomeDir
}

func dataHome() string {
	d := os.Getenv("XDG_DATA_HOME")
	if d == "" {
		if runtime.GOOS == "windows" && localAppData() != "" {
			d = localAppData()
		} else {
			d = filepath.Join(HomeDir, ".local", "share")
		}
	}
	return filepath.Join(d, "heroku")
}

func loadNewCLI() {
	bin := filepath.Join(DataHome, "cli", "bin", "heroku")
	if exists, _ := fileExists(bin); !exists {
		return
	}
	if runtime.GOOS == "windows" {
		cmd := exec.Command(bin, os.Args[1:]...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			os.Exit(getExitCode(err))
		}
	} else {
		if err := syscall.Exec(bin, os.Args[1:], os.Environ()); err != nil {
			panic(err)
		}
	}
}

func fileExists(path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func getExitCode(err error) int {
	switch e := err.(type) {
	case nil:
		return 0
	case *exec.ExitError:
		status, ok := e.Sys().(syscall.WaitStatus)
		if !ok {
			panic(err)
		}
		return status.ExitStatus()
	default:
		panic(err)
	}
}

func update() {
	log.Println("Updating CLI...")
	manifest := getUpdateManifest("dev")
	build := manifest.Builds[runtime.GOOS+"-"+runtime.GOARCH]
	reader, getSha, err := downloadXZ(build.URL)
	must(err)
	if getSha() != build.Sha256 {
		panic(fmt.Errorf("SHA mismatch"))
	}
	tmp := tmpDir(DataHome)
	must(extractTar(reader, tmp))
	must(os.Rename(filepath.Join(DataHome, "cli"), filepath.Join(tmpDir(DataHome), "heroku")))
	must(os.Rename(filepath.Join(tmp, "heroku"), filepath.Join(DataHome, "cli")))
	log.Println("done")
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func getUpdateManifest(channel string) *Manifest {
	res, err := goreq.Request{
		Uri:     "https://cli-assets.heroku.com/" + channel + "/manifest.json",
		Timeout: 30 * time.Minute,
	}.Do()
	must(err)
	var m Manifest
	res.Body.FromJsonTo(&m)
	return &m
}

func downloadXZ(url string) (io.Reader, func() string, error) {
	req := goreq.Request{Uri: url, Timeout: 30 * time.Minute}
	resp, err := req.Do()
	if err != nil {
		return nil, nil, err
	}
	if err := getHTTPError(resp); err != nil {
		return nil, nil, err
	}
	getSha, reader := computeSha(resp.Body)
	uncompressed, err := xz.NewReader(reader)
	return uncompressed, getSha, err
}

func getHTTPError(resp *goreq.Response) error {
	if resp.StatusCode < 400 {
		return nil
	}
	var body string
	body = resp.Header.Get("Content-Type")
	return fmt.Errorf("%s: %s", resp.Status, body)
}

func computeSha(reader io.Reader) (func() string, io.Reader) {
	hasher := sha256.New()
	tee := io.TeeReader(reader, hasher)
	getSha := func() string {
		ioutil.ReadAll(tee)
		return hex.EncodeToString(hasher.Sum(nil))
	}
	return getSha, tee
}

func tmpDir(base string) string {
	root := filepath.Join(base, "tmp")
	err := os.MkdirAll(root, 0755)
	if err != nil {
		panic(err)
	}
	dir, err := ioutil.TempDir(root, "")
	if err != nil {
		panic(err)
	}
	return dir
}
