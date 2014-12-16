// Copyright (c) 2014, B3log
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package conf includes configurations related manipulations, all configurations (including user configurations) are
// stored in wide.json.
package conf

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/b3log/wide/event"
	"github.com/b3log/wide/log"
	"github.com/b3log/wide/util"
)

const (
	// PathSeparator holds the OS-specific path separator.
	PathSeparator = string(os.PathSeparator)
	// PathListSeparator holds the OS-specific path list separator.
	PathListSeparator = string(os.PathListSeparator)
)

const (
	// WideVersion holds the current wide version.
	WideVersion = "1.1.0"
	// CodeMirrorVer holds the current editor version.
	CodeMirrorVer = "4.8"
)

// LatestSessionContent represents the latest session content.
type LatestSessionContent struct {
	FileTree    []string // paths of expanding nodes of file tree
	Files       []string // paths of files of opening editor tabs
	CurrentFile string   // path of file of the current focused editor tab
}

// User configuration.
type User struct {
	Name                 string
	Password             string
	Email                string
	Gravatar             string // see http://gravatar.com
	Workspace            string // the GOPATH of this user
	Locale               string
	GoFormat             string
	FontFamily           string
	FontSize             string
	Theme                string
	Editor               *Editor
	LatestSessionContent *LatestSessionContent
}

// Editor configuration of a user.
type Editor struct {
	FontFamily string
	FontSize   string
	LineHeight string
	Theme      string
	TabSize    string
}

// Configuration.
type conf struct {
	IP                    string  // server ip, ${ip}
	Port                  string  // server port
	Context               string  // server context
	Server                string  // server host and port ({IP}:{Port})
	StaticServer          string  // static resources server scheme, host and port (http://{IP}:{Port})
	LogLevel              string  // logging level
	Channel               string  // channel (ws://{IP}:{Port})
	HTTPSessionMaxAge     int     // HTTP session max age (in seciond)
	StaticResourceVersion string  // version of static resources
	MaxProcs              int     // Go max procs
	RuntimeMode           string  // runtime mode (dev/prod)
	WD                    string  // current working direcitory, ${pwd}
	Locale                string  // default locale
	Users                 []*User // configurations of users
}

// Configuration variable.
var Wide conf

// A raw copy of configuration variable.
//
// Save function will use this variable to persist.
var rawWide conf

// Logger.
var logger = log.NewLogger(os.Stdout)

// NewUser creates a user with the specified username, password, email and workspace.
func NewUser(username, password, email, workspace string) *User {
	hash := md5.New()
	hash.Write([]byte(email))
	gravatar := hex.EncodeToString(hash.Sum(nil))

	return &User{Name: username, Password: password, Email: email, Gravatar: gravatar, Workspace: workspace,
		Locale: Wide.Locale, GoFormat: "gofmt", FontFamily: "Helvetica", FontSize: "13px", Theme: "default",
		Editor: &Editor{FontFamily: "Consolas, 'Courier New', monospace", FontSize: "inherit", LineHeight: "17px",
			Theme: "wide", TabSize: "4"}}
}

// Load loads the configurations from wide.json.
func Load(confPath, confIP, confPort, confServer, confLogLevel, confStaticServer, confContext, confChannel string,
	confDocker bool) {
	bytes, _ := ioutil.ReadFile(confPath)

	err := json.Unmarshal(bytes, &Wide)
	if err != nil {
		logger.Error("Parses wide.json error: ", err)

		os.Exit(-1)
	}

	log.SetLevel(Wide.LogLevel)

	// keep the raw content
	json.Unmarshal(bytes, &rawWide)

	logger.Debug("Conf: \n" + string(bytes))

	// Working Driectory
	Wide.WD = util.OS.Pwd()
	logger.Debugf("${pwd} [%s]", Wide.WD)

	// IP
	ip, err := util.Net.LocalIP()
	if err != nil {
		logger.Error(err)

		os.Exit(-1)
	}

	logger.Debugf("${ip} [%s]", ip)

	if confDocker {
		// TODO: may be we need to do something here
	}

	if "" != confIP {
		ip = confIP
	}

	Wide.IP = strings.Replace(Wide.IP, "${ip}", ip, 1)

	if "" != confPort {
		Wide.Port = confPort
	}

	// Server
	Wide.Server = strings.Replace(Wide.Server, "{IP}", Wide.IP, 1)
	if "" != confServer {
		Wide.Server = confServer
	}

	// Logging Level
	if "" != confLogLevel {
		Wide.LogLevel = confLogLevel
		log.SetLevel(confLogLevel)
	}

	// Static Server
	Wide.StaticServer = strings.Replace(Wide.StaticServer, "{IP}", Wide.IP, 1)
	if "" != confStaticServer {
		Wide.StaticServer = confStaticServer
	}

	// Context
	if "" != confContext {
		Wide.Context = confContext
	}

	Wide.StaticResourceVersion = strings.Replace(Wide.StaticResourceVersion, "${time}", strconv.FormatInt(time.Now().UnixNano(), 10), 1)

	// Channel
	Wide.Channel = strings.Replace(Wide.Channel, "{IP}", Wide.IP, 1)
	Wide.Channel = strings.Replace(Wide.Channel, "{Port}", Wide.Port, 1)
	if "" != confChannel {
		Wide.Channel = confChannel
	}

	Wide.Server = strings.Replace(Wide.Server, "{Port}", Wide.Port, 1)
	Wide.StaticServer = strings.Replace(Wide.StaticServer, "{Port}", Wide.Port, 1)

	// upgrade if need
	upgrade()

	initWorkspaceDirs()
	initCustomizedConfs()
}

// FixedTimeCheckEnv checks Wide runtime enviorment periodically (7 minutes).
//
// Exits process if found fatal issues (such as not found $GOPATH),
// Notifies user by notification queue if found warning issues (such as not found gocode).
func FixedTimeCheckEnv() {
	checkEnv() // check immediately

	go func() {
		for _ = range time.Tick(time.Minute * 7) {
			checkEnv()
		}
	}()
}

func checkEnv() {
	cmd := exec.Command("go", "version")
	buf, err := cmd.CombinedOutput()
	if nil != err {
		logger.Error("Not found 'go' command, please make sure Go has been installed correctly")

		os.Exit(-1)
	}
	logger.Trace(string(buf))

	if "" == os.Getenv("GOPATH") {
		logger.Error("Not found $GOPATH, please configure it before running Wide")

		os.Exit(-1)
	}

	gocode := util.Go.GetExecutableInGOBIN("gocode")
	cmd = exec.Command(gocode, "close")
	_, err = cmd.Output()
	if nil != err {
		event.EventQueue <- &event.Event{Code: event.EvtCodeGocodeNotFound}

		logger.Warnf("Not found gocode [%s]", gocode)
	}

	ideStub := util.Go.GetExecutableInGOBIN("ide_stub")
	cmd = exec.Command(ideStub, "version")
	_, err = cmd.Output()
	if nil != err {
		event.EventQueue <- &event.Event{Code: event.EvtCodeIDEStubNotFound}

		logger.Warnf("Not found ide_stub [%s]", ideStub)
	}
}

// FixedTimeSave saves configurations (wide.json) periodically (1 minute).
//
// Main goal of this function is to save user session content, for restoring session content while user open Wide next time.
func FixedTimeSave() {
	go func() {
		for _ = range time.Tick(time.Minute) {
			Save()
		}
	}()
}

// GetUserWorkspace gets workspace path with the specified username, returns "" if not found.
func (c *conf) GetUserWorkspace(username string) string {
	for _, user := range c.Users {
		if user.Name == username {
			return user.GetWorkspace()
		}
	}

	return ""
}

// GetGoFmt gets the path of Go format tool, returns "gofmt" if not found "goimports".
func (c *conf) GetGoFmt(username string) string {
	for _, user := range c.Users {
		if user.Name == username {
			switch user.GoFormat {
			case "gofmt":
				return "gofmt"
			case "goimports":
				return util.Go.GetExecutableInGOBIN("goimports")
			default:
				logger.Errorf("Unsupported Go Format tool [%s]", user.GoFormat)
				return "gofmt"
			}
		}
	}

	return "gofmt"
}

// GetWorkspace gets workspace path of the user.
//
// Compared to the use of Wide.Workspace, this function will be processed as follows:
//  1. Replace {WD} variable with the actual directory path
//  2. Replace ${GOPATH} with enviorment variable GOPATH
//  3. Replace "/" with "\\" (Windows)
func (u *User) GetWorkspace() string {
	w := strings.Replace(u.Workspace, "{WD}", Wide.WD, 1)
	w = strings.Replace(w, "${GOPATH}", os.Getenv("GOPATH"), 1)

	return filepath.FromSlash(w)
}

// GetUser gets configuration of the user specified by the given username, returns nil if not found.
func (*conf) GetUser(username string) *User {
	for _, user := range Wide.Users {
		if user.Name == username {
			return user
		}
	}

	return nil
}

// Save saves Wide configurations.
func Save() bool {
	// just the Users field are volatile
	rawWide.Users = Wide.Users

	// format
	bytes, err := json.MarshalIndent(rawWide, "", "    ")

	if nil != err {
		logger.Error(err)

		return false
	}

	if err = ioutil.WriteFile("conf/wide.json", bytes, 0644); nil != err {
		logger.Error(err)

		return false
	}

	return true
}

// upgrade upgrades the wide.json.
func upgrade() {
	// Users
	for _, user := range Wide.Users {
		if "" == user.Theme {
			user.Theme = "default" // since 1.1.0
		}

		if "" == user.Editor.Theme {
			user.Editor.Theme = "wide" // since 1.1.0
		}

		if "" == user.Editor.TabSize {
			user.Editor.TabSize = "4" // since 1.1.0
		}

		if "" != user.Email && "" == user.Gravatar {
			hash := md5.New()
			hash.Write([]byte(user.Email))
			gravatar := hex.EncodeToString(hash.Sum(nil))

			user.Gravatar = gravatar
		}
	}

	Save()
}

// initCustomizedConfs initializes the user customized configurations.
func initCustomizedConfs() {
	for _, user := range Wide.Users {
		UpdateCustomizedConf(user.Name)
	}
}

// UpdateCustomizedConf creates (if not exists) or updates user customized configuration files.
//
//  1. /static/user/{username}/style.css
func UpdateCustomizedConf(username string) {
	var u *User
	for _, user := range Wide.Users { // maybe it is a beauty of the trade-off of the another world between design and implementation
		if user.Name == username {
			u = user
		}
	}

	if nil == u {
		return
	}

	model := map[string]interface{}{"user": u}

	t, err := template.ParseFiles("static/user/style.css.tmpl")
	if nil != err {
		logger.Error(err)

		os.Exit(-1)
	}

	wd := util.OS.Pwd()
	dir := filepath.Clean(wd + "/static/user/" + u.Name)
	if err := os.MkdirAll(dir, 0755); nil != err {
		logger.Error(err)

		os.Exit(-1)
	}

	fout, err := os.Create(dir + PathSeparator + "style.css")
	if nil != err {
		logger.Error(err)

		os.Exit(-1)
	}

	defer fout.Close()

	if err := t.Execute(fout, model); nil != err {
		logger.Error(err)

		os.Exit(-1)
	}
}

// initWorkspaceDirs initializes the directories of users' workspaces.
//
// Creates directories if not found on path of workspace.
func initWorkspaceDirs() {
	paths := []string{}

	for _, user := range Wide.Users {
		paths = append(paths, filepath.SplitList(user.GetWorkspace())...)
	}

	for _, path := range paths {
		CreateWorkspaceDir(path)
	}
}

// CreateWorkspaceDir creates (if not exists) directories on the path.
//
//  1. root directory:{path}
//  2. src directory: {path}/src
//  3. package directory: {path}/pkg
//  4. binary directory: {path}/bin
func CreateWorkspaceDir(path string) {
	createDir(path)
	createDir(path + PathSeparator + "src")
	createDir(path + PathSeparator + "pkg")
	createDir(path + PathSeparator + "bin")
}

// createDir creates a directory on the path if it not exists.
func createDir(path string) {
	if !util.File.IsExist(path) {
		if err := os.MkdirAll(path, 0775); nil != err {
			logger.Error(err)

			os.Exit(-1)
		}
	}
}

// GetEditorThemes gets the names of editor themes.
func GetEditorThemes() []string {
	ret := []string{}

	f, _ := os.Open("static/js/overwrite/codemirror" + "/theme")
	names, _ := f.Readdirnames(-1)
	f.Close()

	for _, name := range names {
		ret = append(ret, name[:strings.LastIndex(name, ".")])
	}

	sort.Strings(ret)

	return ret
}

// GetThemes gets the names of themes.
func GetThemes() []string {
	ret := []string{}

	f, _ := os.Open("static/css/themes")
	names, _ := f.Readdirnames(-1)
	f.Close()

	for _, name := range names {
		ret = append(ret, name[:strings.LastIndex(name, ".")])
	}

	sort.Strings(ret)

	return ret
}
