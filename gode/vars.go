/*
Package gode runs a sandboxed node installation to run node code and install npm packages.
*/
package gode

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

var registry string
var rootPath string
var modulesDir string
var nodeBinPath string
var npmBinPath string

func init() {
	registry = os.Getenv("HEROKU_NPM_REGISTRY")
	if registry == "" {
		registry = "https://cli-npm.heroku.com"
	}
}

// SetRootPath sets the root for gode
func SetRootPath(root string) {
	rootPath = root
	modulesDir = filepath.Join(rootPath, "node_modules")
	nodeBinPath = os.Getenv("HEROKU_NODE_PATH")
	if nodeBinPath == "" {
		nodeBinPath = filepath.Join(rootPath, "node")
		if runtime.GOOS == "windows" {
			nodeBinPath += ".exe"
		}
		if exists, _ := fileExists(nodeBinPath); !exists {
			var err error
			nodeBinPath, err = exec.LookPath("node")
			if err != nil {
				panic(err)
			}
		}
	}
	npmBinPath = filepath.Join(rootPath, "npm", "cli.js")
}
