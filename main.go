package main

import (
	"encoding/json"
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

	"github.com/dickeyxxx/golock"
	"github.com/franela/goreq"
	"github.com/ulikunitz/xz"
)

var Channel = "dev"

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

func init() {
	goreq.SetConnectTimeout(15 * time.Second)
}

func main() {
	runtime.GOMAXPROCS(1)
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

func newCLIPath() string {
	path := filepath.Join(DataHome, "cli", "bin", "heroku")
	if runtime.GOOS == "windows" {
		path = path + ".exe"
	}
	return path
}

func loadNewCLI() {
	if exists, _ := fileExists(newCLIPath()); !exists {
		return
	}
	if runtime.GOOS == "windows" {
		cmd := exec.Command(newCLIPath(), os.Args[1:]...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		os.Exit(getExitCode(err))
	} else {
		if err := syscall.Exec(newCLIPath(), os.Args, os.Environ()); err != nil {
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
	lockpath := filepath.Join(DataHome, "tmp", "updating")
	if locked, err := golock.IsLocked(lockpath); locked || err != nil {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		} else {
			fmt.Fprintln(os.Stderr, "update in progress")
			golock.Lock(lockpath)
			return
		}
	}
	if err := golock.Lock(lockpath); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Stderr.WriteString("heroku-cli: Updating CLI...")
	manifest := getUpdateManifest(Channel)
	os.Stderr.WriteString(fmt.Sprintf("\rheroku-cli: Updating to %s...", manifest.Version))
	build := manifest.Builds[runtime.GOOS+"-"+runtime.GOARCH]
	reader, err := downloadXZ(build.URL)
	must(err)
	tmp := tmpDir(DataHome)
	must(extractTar(reader, tmp))
	tmp2 := tmpDir(DataHome)
	os.Rename(filepath.Join(DataHome, "cli"), filepath.Join(tmp2, "cli"))
	must(os.Rename(filepath.Join(tmp, "heroku"), filepath.Join(DataHome, "cli")))
	os.Remove(tmp)
	os.Remove(tmp2)
	os.Stderr.WriteString(" done\n")

	os.MkdirAll(filepath.Join(configHome()), 0755)
	copyfile(filepath.Join(legacyhome(), "config.json"), filepath.Join(configHome(), "config.json"))
	copyplugins()
}

func copyfile(src, dst string) {
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return
	}
	defer out.Close()
	io.Copy(out, in)
}

var coreplugins = []string{
	"heroku-apps",
	"heroku-cli-addons",
	"heroku-fork",
	"heroku-git",
	"heroku-local",
	"heroku-orgs",
	"heroku-pipelines",
	"heroku-run",
	"heroku-spaces",
	"heroku-status",
}

func include(arr []string, i string) bool {
	for _, j := range arr {
		if i == j {
			return true
		}
	}
	return false
}

func copyplugins() {
	symlinked := func(name string) bool {
		path := filepath.Join(legacyhome(), "node_modules", name)
		fi, err := os.Lstat(path)
		if err != nil {
			return true
		}
		return fi.Mode()&os.ModeSymlink != 0

	}
	plugins, err := readJSON(filepath.Join(legacyhome(), "plugin-cache.json"))
	if err != nil {
		log.Println(err)
		return
	}
	tocopy := []string{}
	for name, plugin := range plugins {
		if include(coreplugins, name) || symlinked(name) {
			continue
		}
		commands, ok := plugin.(map[string]interface{})["commands"].([]interface{})
		if !ok {
			continue
		}
		if len(commands) != 0 {
			tocopy = append(tocopy, name)
		}
	}

	if len(tocopy) == 0 {
		return
	}

	os.Stderr.WriteString("heroku-cli: updating plugins...\n")
	for _, plugin := range tocopy {
		cmd := exec.Command(newCLIPath(), "plugins:install", plugin)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			os.Exit(getExitCode(err))
		}
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func getUpdateManifest(channel string) *Manifest {
	if channel == "master" {
		channel = "stable"
	}
	res, err := goreq.Request{
		Uri:     "https://cli-assets.heroku.com/branches/" + channel + "/manifest.json",
		Timeout: 30 * time.Minute,
	}.Do()
	must(err)
	var m Manifest
	res.Body.FromJsonTo(&m)
	return &m
}

func downloadXZ(url string) (io.Reader, error) {
	req := goreq.Request{Uri: url, Timeout: 30 * time.Minute}
	resp, err := req.Do()
	if err != nil {
		return nil, err
	}
	if err := getHTTPError(resp); err != nil {
		return nil, err
	}
	uncompressed, err := xz.NewReader(resp.Body)
	return uncompressed, err
}

func getHTTPError(resp *goreq.Response) error {
	if resp.StatusCode < 400 {
		return nil
	}
	var body string
	body = resp.Header.Get("Content-Type")
	return fmt.Errorf("%s: %s", resp.Status, body)
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

func legacyhome() string {
	if runtime.GOOS == "windows" {
		dir := os.Getenv("LOCALAPPDATA")
		if dir != "" {
			return filepath.Join(dir, "heroku")
		}
	}
	dir := os.Getenv("XDG_DATA_HOME")
	if dir != "" {
		return filepath.Join(dir, "heroku")
	}
	return filepath.Join(HomeDir, ".heroku")
}

func configHome() string {
	d := os.Getenv("XDG_CONFIG_HOME")
	if d == "" {
		d = filepath.Join(HomeDir, ".config")
	}
	return filepath.Join(d, "heroku")
}

func readJSON(path string) (out map[string]interface{}, err error) {
	if exists, err := fileExists(path); !exists {
		if err != nil {
			panic(err)
		}
		return map[string]interface{}{}, nil
	}
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(data, &out)
	return out, err
}
