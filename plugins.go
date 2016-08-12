package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ansel1/merry"
	"github.com/dickeyxxx/golock"
)

// Plugins represents either core or user plugins
type Plugins struct {
	Path    string
	plugins []*Plugin
}

// CorePlugins are built in plugins
var CorePlugins = &Plugins{Path: filepath.Join(AppDir, "lib")}

// UserPlugins are user-installable plugins
var UserPlugins = &Plugins{Path: filepath.Join(DataHome, "plugins")}

// Plugin represents a javascript plugin
type Plugin struct {
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	Topics    Topics    `json:"topics"`
	Topic     *Topic    `json:"topic"`
	Commands  Commands  `json:"commands"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Commands lists all the commands of the plugins
func (p *Plugins) Commands() (commands Commands) {
	for _, plugin := range p.Plugins() {
		for _, command := range plugin.Commands {
			command.Run = p.runFn(plugin, command.Topic, command.Command)
			commands = append(commands, command)
		}
	}
	return
}

// Topics gets all the plugin's topics
func (p *Plugins) Topics() (topics Topics) {
	for _, plugin := range p.Plugins() {
		if plugin.Topic != nil {
			topics = append(topics, plugin.Topic)
		}
		topics = append(topics, plugin.Topics...)
	}
	return
}

func (p *Plugins) runFn(plugin *Plugin, topic, command string) func(ctx *Context) {
	return func(ctx *Context) {
		p.readLockPlugin(plugin.Name)
		ctx.Dev = p.isPluginSymlinked(plugin.Name)
		ctxJSON, err := json.Marshal(ctx)
		must(err)
		args, err := json.Marshal(Args)
		must(err)

		script := fmt.Sprintf(`'use strict'
process.argv = %s
let pluginName = '%s'
let pluginVersion = '%s'
let topic = '%s'
let command = '%s'
let ctx = %s
ctx.version = ctx.version + ' ' + pluginName + '/' + pluginVersion + ' node-' + process.version
if (command === '') { command = null }
let plugin = require(pluginName)
let cmd = plugin.commands.filter((c) => c.topic === topic && c.command == command)[0]

function handleEPIPE (err) {
	if (err.errno !== 'EPIPE') throw err
}
process.stdout.on('error', handleEPIPE)
process.stderr.on('error', handleEPIPE)

cmd.run(ctx)
`, args, plugin.Name, plugin.Version, topic, command, ctxJSON)

		// swallow sigint since the plugin will handle it
		swallowSigint = true

		if ctx.Dev {
			currentAnalyticsCommand = nil
		} else {
			currentAnalyticsCommand.Plugin = plugin.Name
			currentAnalyticsCommand.PluginVersion = plugin.Version
			currentAnalyticsCommand.Language = "node"
		}

		cmd, done := p.RunScript(script)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		done()
		Exit(getExitCode(err))
	}
}

func getExitCode(err error) int {
	switch e := err.(type) {
	case nil:
		return 0
	case *exec.ExitError:
		status, ok := e.Sys().(syscall.WaitStatus)
		if !ok {
			must(err)
		}
		return status.ExitStatus()
	}
	must(err)
	return -1
}

// ParsePlugin requires the plugin's node module
// to get the commands and metadata
func (p *Plugins) ParsePlugin(name string) (*Plugin, error) {
	script := `
	var plugin = require('` + name + `')
	var pjson  = require('` + name + `/package.json')

	plugin.name    = pjson.name
	plugin.version = pjson.version

	console.log(JSON.stringify(plugin))`
	cmd, done := p.RunScript(script)
	cmd.Stderr = Stderr
	output, err := cmd.Output()
	done()

	if err != nil {
		return nil, merry.Errorf("Error installing plugin %s", name)
	}
	var plugin Plugin
	plugin.UpdatedAt = time.Now()
	err = json.Unmarshal(output, &plugin)
	if err != nil {
		return nil, fmt.Errorf("Error parsing plugin: %s\n%s\n%s\nIs this a real CLI plugin?", name, err, string(output))
	}
	if len(plugin.Commands) == 0 {
		return nil, fmt.Errorf("Invalid plugin. No commands found.")
	}
	for _, command := range plugin.Commands {
		if command == nil {
			continue
		}
		command.Plugin = plugin.Name
		command.Help = strings.TrimSpace(command.Help)
	}
	p.addToCache(&plugin)
	return &plugin, nil
}

// PluginNames lists all the plugin names
func (p *Plugins) PluginNames() []string {
	plugins := p.Plugins()
	names := make([]string, 0, len(plugins))
	for _, plugin := range plugins {
		names = append(names, plugin.Name)
	}
	return names
}

// PluginNamesNotSymlinked lists all the plugin names that are not symlinked
func (p *Plugins) PluginNamesNotSymlinked() []string {
	plugins := p.PluginNames()
	names := make([]string, 0, len(plugins))
	for _, plugin := range plugins {
		if !p.isPluginSymlinked(plugin) {
			names = append(names, plugin)
		}
	}
	return names
}

func (p *Plugins) isPluginSymlinked(plugin string) bool {
	path := filepath.Join(p.modulesPath(), plugin)
	fi, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeSymlink != 0
}

func contains(arr []string, s string) bool {
	for _, a := range arr {
		if a == s {
			return true
		}
	}
	return false
}

// InstallPlugins installs plugins
func (p *Plugins) InstallPlugins(names ...string) error {
	for _, name := range names {
		p.lockPlugin(name)
	}
	defer func() {
		for _, name := range names {
			p.unlockPlugin(name)
		}
	}()
	err := p.installPackages(names...)
	if err != nil {
		return err
	}
	for _, name := range names {
		_, err := p.ParsePlugin(name)
		must(err)
	}
	return nil
}

// directory location of plugin
func (p *Plugins) pluginPath(plugin string) string {
	return filepath.Join(p.Path, "node_modules", plugin)
}

// name of lockfile
func (p *Plugins) lockfile(name string) string {
	return filepath.Join(p.Path, name+".updating")
}

// lock a plugin for reading
func (p *Plugins) readLockPlugin(name string) {
	locked, err := golock.IsLocked(p.lockfile(name))
	LogIfError(err)
	if locked {
		p.lockPlugin(name)
		p.unlockPlugin(name)
	}
}

// lock a plugin for writing
func (p *Plugins) lockPlugin(name string) {
	os.MkdirAll(filepath.Dir(p.lockfile(name)), 0755)
	LogIfError(golock.Lock(p.lockfile(name)))
}

// unlock a plugin
func (p *Plugins) unlockPlugin(name string) {
	LogIfError(golock.Unlock(p.lockfile(name)))
}

// Update updates the plugins
func (p *Plugins) Update() {
	plugins := p.PluginNamesNotSymlinked()
	if len(plugins) == 0 {
		return
	}
	packages, err := p.OutdatedPackages(plugins...)
	WarnIfError(err)
	if len(packages) > 0 {
		action("heroku-cli: Updating plugins", "", func() {
			for name, version := range packages {
				p.lockPlugin(name)
				WarnIfError(p.installPackages(name + "@" + version))
				_, err := p.ParsePlugin(name)
				WarnIfError(err)
				p.unlockPlugin(name)
			}
		})
		Errf(" done. Updated %d %s.\n", len(packages), plural("package", len(packages)))
	}
}

// MigrateRubyPlugins migrates from legacy ruby plugins to node versions
func (p *Plugins) MigrateRubyPlugins() {
	pluginMap := map[string]string{
		"heroku-accounts":  "heroku-accounts",
		"heroku-buildkits": "heroku-buildkits",
		"heroku-config":    "heroku-config",
		"heroku-oauth":     "heroku-cli-oauth",
		"heroku-pg-extras": "heroku-pg-extras",
		"heroku-repo":      "heroku-repo",
		"heroku-run-local": "heroku-run-localjs",
		"heroku-vim":       "heroku-vim",
	}
	for _, ruby := range RubyPlugins() {
		plugin := pluginMap[ruby]
		if plugin == "" || contains(p.PluginNames(), plugin) {
			continue
		}
		action("Updating "+plugin+" plugin", "done", func() {
			WarnIfError(p.InstallPlugins(plugin))
		})
	}
}

func (p *Plugins) addToCache(plugin *Plugin) {
	contains := func(name string) int {
		for i, plugin := range p.plugins {
			if plugin.Name == name {
				return i
			}
		}
		return -1
	}
	// find or replace
	i := contains(plugin.Name)
	if i == -1 {
		p.plugins = append(p.plugins, plugin)
	} else {
		p.plugins[i] = plugin
	}
	p.saveCache()
}

func (p *Plugins) removeFromCache(name string) {
	for i, plugin := range p.plugins {
		if plugin.Name == name {
			p.plugins = append(p.plugins[:i], p.plugins[i+1:]...)
		}
	}
	p.saveCache()
}

func (p *Plugins) saveCache() {
	if err := saveJSON(p.plugins, p.cachePath()); err != nil {
		must(err)
	}
}

// Plugins reads the cache file into the struct
func (p *Plugins) Plugins() []*Plugin {
	if p.plugins == nil {
		p.plugins = []*Plugin{}
		if exists, _ := FileExists(p.cachePath()); !exists {
			return p.plugins
		}
		f, err := os.Open(p.cachePath())
		if err != nil {
			LogIfError(err)
			return p.plugins
		}
		err = json.NewDecoder(f).Decode(&p.plugins)
		WarnIfError(err)
		p.removeMissingPlugins()
		p.RefreshPlugins()
	}
	return p.plugins
}

func (p *Plugins) removeMissingPlugins() {
	for i, plugin := range p.plugins {
		if exists, _ := FileExists(p.pluginPath(plugin.Name)); !exists {
			p.plugins = append(p.plugins[:i], p.plugins[i+1:]...)
			p.saveCache()
			p.removeMissingPlugins()
			return
		}
	}
}

func (p *Plugins) cachePath() string {
	return filepath.Join(p.Path, "plugins.json")
}

// RubyPlugins lists all the ruby plugins
func RubyPlugins() []string {
	dirs, err := ioutil.ReadDir(filepath.Join(HomeDir, ".heroku", "plugins"))
	if err != nil {
		return []string{}
	}
	plugins := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		plugins = append(plugins, dir.Name())
	}
	return plugins
}

// ByName returns a plugin by its name
func (p *Plugins) ByName(name string) *Plugin {
	for _, plugin := range p.Plugins() {
		if plugin.Name == name {
			return plugin
		}
	}
	return nil
}

// RefreshPlugins reparses plugin's metadata if symlinked and has modified files
func (p *Plugins) RefreshPlugins() {
	for _, plugin := range p.plugins {
		if !p.pluginRefreshNeeded(plugin) {
			continue
		}
		action(fmt.Sprintf("Parsing %s", plugin.Name), "done", func() {
			_, err := p.ParsePlugin(plugin.Name)
			must(err)
		})
	}
}

// returns true if symlinked and any files in the plugin are newer than the cached version
func (p *Plugins) pluginRefreshNeeded(plugin *Plugin) bool {
	if !p.isPluginSymlinked(plugin.Name) {
		return false
	}
	base, err := filepath.EvalSymlinks(p.pluginPath(plugin.Name))
	must(err)
	skip := func(path string) bool {
		for _, dir := range []string{".git", "node_modules"} {
			if strings.HasSuffix(path, dir) {
				return true
			}
		}
		return false
	}
	refresh := false
	filepath.Walk(base, func(path string, fi os.FileInfo, err error) error {
		if skip(path) || refresh {
			return filepath.SkipDir
		}
		if fi.ModTime().After(plugin.UpdatedAt) {
			refresh = true
		}
		return nil
	})
	return refresh
}
