package main

/*
  CPM - C/C++ Package Manager
  (c) Mircea Neacsu 2021-2026

  This tool uses simple JSON files to manage dependencies between
  different packages.

  Usage:
    cpm [options] [<package>]
    or
      cpm version

  If package name is missing, the program assumes to be the current
  directory.

  Valid options are:
    -b <branch name> switches to specific branch or tag
    -F discards local changes when switching branches
    -f fetch-only (do not build)
    -l local-only (do not pull)
    -v verbose
    --root <rootdir> (or -r <rootdir>) - root directory of development tree
    --uri <uri> (or -u <uri>) - URI of root package
    --proto [git | https] - protocol used for cloning
    --version  - show version

  The program opens the '<rootdir>/<package>/cpm.json' file and
  recursively searches and builds all dependencies.

  Default root of development tree is the ${DEV_ROOT} environment variable.
*/

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

const Version = "V0.7.1"

type Command struct {
	Os   string
	Cmd  string
	Args []string
}

type DependencyDescriptor struct {
	Name      string
	Uri       string
	Branch    string
	Modules   []string
	FetchOnly bool
	Post      []Command
	pack      *PacUnit
}

type PacUnit struct {
	Name    string
	Uri     string
	Branch  string
	Build   []Command
	Depends []DependencyDescriptor
	built   bool
}

var devroot string         //root of development tree
const descriptor_name = "cpm.json"

var all_packs []*PacUnit

var inprocess []string

// command line flags
var force_flag = flag.Bool("F", false, "discard local changes")
var fetch_flag = flag.Bool("f", false, "fetch only (no build)")
var local_flag = flag.Bool("l", false, "local only (no pull)")
var verbose_flag = flag.Bool("v", false, "verbose")
var branch_flag = flag.String("b", "", "select branch")

func main() {
	var err error
	var show_ver bool
	var uri string

	println("C/C++ Package Manager " + Version)
	flag.StringVar(&uri, "uri", "", "root URI")
	flag.StringVar(&uri, "u", "", "root URI")
	flag.StringVar(&devroot, "r", os.Getenv("DEV_ROOT"), "development tree root")
	flag.StringVar(&devroot, "root", os.Getenv("DEV_ROOT"), "development tree root")
	flag.BoolVar(&show_ver, "version", false, "show version")
	start := time.Now()
	flag.Usage = func() {
		println(`Usage: 
		cpm [options] [package]
	or
		cpm version

  If package is not specified, it is assumed to be the current directory.
  Valid options are:
    -b <branch name>          	checkout specific branch or tag
		-F                          discards local changes when switching branches
    -f                        	fetch-only (no build)
    -l                        	local-only (no fetch/pull)
    --root <dir> (or -r <dir>)  set root of development tree
    --uri <uri> (or -u <uri>)	  URI of root package
    -v                        	verbose
		--version                   show program version and exit
    --help (or -h)            	prints this message`)
	}

	flag.Parse()
	if show_ver || (flag.NFlag() == 0 && flag.NArg() > 0 && flag.Arg(0) == "version") {
		os.Exit(0)
	}

	//Sanity checks
	if uri != "" && *local_flag {
		log.Fatal("Local mode only. Cannot fetch root package!!")
	}

	if strings.ContainsAny(flag.Arg(0), "\\/") {
		log.Fatalf("Root package cannot have a path - %s", flag.Arg(0))
	}

	var root = new (PacUnit)
	if devroot == "" {
		//Find a reasonable default for DEV_ROOT
		if uri == "" && flag.NArg() == 0 {
			devroot = ".."
		} else {
			devroot = "."
		}
	}

	if !filepath.IsAbs(devroot) {
		devroot, err = filepath.Abs(devroot)
		if err != nil {
			log.Fatalf("Cannot find DEV_ROOT absolute path DEV_ROOT=%s - %v", devroot, err)
		}
	}
	Verboseln("DEV_ROOT=", devroot)

	if uri != "" {
		// URI specified -> try to clone or pull root package
		if flag.NArg() > 0 {
			root_dir := filepath.Join(devroot, flag.Arg(0))
			stat,err := os.Stat(root_dir)
			if os.IsNotExist(err) {
				git_clone (uri, *branch_flag, root_dir)
				os.Chdir(root_dir)
				parse_descriptor(root)
			} else if stat.IsDir() {
				if git_is_repo(root_dir) {
					os.Chdir(root_dir)
					git_pull (*branch_flag)
					parse_descriptor (root)
				} else if ret,_:= IsEmptyDir (root_dir); ret {
					git_clone (uri, *branch_flag, root_dir) //empty directory already exists
					os.Chdir(root_dir)
					parse_descriptor(root)
				} else {
					log.Fatalf ("Directory %s exists and is not empty", root_dir)
				}
			} else {
				log.Fatalf ("%s exists and is not a directory", root_dir)
			}
			root.Name = flag.Arg(0) //
		} else {
			// No root package on command line. 
			// Clone repo indicated by URI
			clone_root(uri, root)
			os.Chdir(filepath.Join(devroot, root.Name))
		}
	} else if (flag.NArg() > 0) {
		//No URI, root package on command line
		root_dir := filepath.Join(devroot, flag.Arg(0))
		rstat,err := os.Stat (root_dir)
		if err != nil {
			log.Fatalf("Cannot stat project directory %s: %v", root_dir, err)
		}
		if !rstat.IsDir() {
			log.Fatalf ("%s exists and is not a directory", root_dir)
		}
		os.Chdir(root_dir)
		git_pull(*branch_flag)
		parse_descriptor(root)
	} else {
		// No URI specified and no root package name on command line
		// Assume there is a CPM descriptor file in current directory.
		parse_descriptor(root)
		rootdir,_ := os.Getwd()
		dir, rdir := filepath.Split(rootdir)
		stat1,_ := os.Stat(devroot)
		stat2,_ := os.Stat(dir)
		if (!os.SameFile(stat1, stat2)) {
			log.Fatal("Current directory is not a child of DEV_ROOT")
		}
		root.Name = rdir
	}


	os.Mkdir(filepath.Join(devroot, "lib"), 0755)
	Symlink(filepath.Join(devroot, "lib"), "lib")

	root.Branch = *branch_flag
	all_packs = append(all_packs, root)

	os.Chdir(filepath.Join(devroot, root.Name))

	cwd, _ := os.Getwd()
	Verboseln("Changed directory to", cwd)

	fetch_dependents(root)

	if !*fetch_flag {
		inprocess = make([]string, 0, 10)
		build(root)
	}

	fmt.Println("CPM operation finished in", time.Since(start).Round(100*time.Microsecond))
}

// Read and parse CPM descriptor file in current directory
func parse_descriptor (p *PacUnit) {
	var data []byte
	var err error
	if data, err = os.ReadFile(descriptor_name); err != nil {
		full_name,_ := filepath.Abs(descriptor_name)
		log.Fatalf("Error: %s - %v", full_name, err)
	}
	if err = json.Unmarshal(data, p); err != nil {		
		full_name,_ := filepath.Abs(descriptor_name)
		log.Fatalf("Cannot parse %s - %v", full_name, err)
	}
}

func get_from_uri (uri string, p *PacUnit) {

}
/*
	Clone root package from given URI
	
	We don't know yet the root package name, so we clone into a temp directory
	and rename it to the actual root package name after parsing the descriptor file.
	If package directory already exists it does only a pull/switch of root repo.
	Checks for the many things that can go wrong during this process.
*/
func clone_root (uri string, root *PacUnit) {
	const temp_dir string = "cpm_temp"
	err := os.Chdir(devroot)
	if err != nil {
		log.Fatalf("Cannot chdir to DEV_ROOT (%s) - %v", devroot, err)
	}
	os.RemoveAll(temp_dir)
	if err = os.Mkdir(temp_dir, 0755); err != nil {
		log.Fatalf("Cannot create temporary directory %s", filepath.Join(devroot, temp_dir))
	}
	git_clone (uri, *branch_flag, temp_dir)

	os.Chdir(temp_dir)
	parse_descriptor(root)
	os.Chdir("..")

	if stat, err := os.Stat(filepath.Join(devroot,root.Name)); err ==  nil {
		if !stat.IsDir() {
				log.Fatalf ("%s exists and is not a directory", root.Name)
		}
		os.RemoveAll(temp_dir)
		os.Chdir(root.Name)
		Verbosef("Root package directory %s already exists\n", filepath.Join(devroot,root.Name))
		git_pull(*branch_flag)
	} else if err = os.Rename(temp_dir, root.Name); err != nil {
		os.RemoveAll(temp_dir)
		log.Fatalf("Cannot rename %s - %v",filepath.Join(devroot, temp_dir), err)
	}
}

// Fetch one package. Changes working directory to the package directory
func fetch(p *PacUnit) {
	pacdir := filepath.Join(devroot, p.Name)

	if _, err := os.Stat(pacdir); os.IsNotExist(err) {
		//package directory doesn't exist; create it and clone repo
		if err := os.Mkdir(pacdir, 0755); err != nil {
			log.Fatalf("error %d - cannot create folder %s", err, pacdir)
		}
		git_clone_package(p)
		os.Chdir(pacdir)
	} else if _, err := os.Stat(filepath.Join(pacdir, ".git")); os.IsNotExist(err) {
		if empty,_ := IsEmptyDir(pacdir); !empty {
			log.Fatalf("Cannot clone %s package. Directory exists and is not empty", p.Name)
		}
		//empty directory exists; clone repo
		git_clone_package(p)
		os.Chdir(pacdir)
	} else {
		//Repo is here. Pull latest version or switch to a specific one
		os.Chdir(pacdir)
		git_pull_package(p)
	}

	Symlink(filepath.Join(devroot, "lib"), "lib")
	cwd, _ := os.Getwd()
	if len(p.Branch) == 0 {
		Verbosef("Setting up %s in %s\n", p.Name, cwd)
	} else {
		Verbosef("Setting up %s@%s in %s\n", p.Name, p.Branch, cwd)
	}

	data, err := os.ReadFile(descriptor_name)
	if err != nil {
		Verbosef(" %s\\%s file not found. Assuming no dependencies\n", cwd, descriptor_name)
	} else {
		if err = json.Unmarshal(data, &p); err != nil {
			log.Fatalf("cannot parse %s - %v", descriptor_name, err)
		}
	}
	fetch_dependents(p)
}

// Fetch all dependent packages
func fetch_dependents(p *PacUnit) {
/*	if p.Depends == nil {
		return
	}
*/		
	cwd, _ := os.Getwd()
	//setup all dependent packages
	for i := range p.Depends {
		var v *PacUnit
		var idx int

		//search if already setup
		found := false
		for idx, v = range all_packs {
			if v.Name == p.Depends[i].Name {
				if v.Branch != p.Depends[i].Branch {
					b1 := v.Branch
					if len(b1) == 0 {
						b1 = "HEAD"
					}
					b2 := p.Depends[i].Branch
					if len(b2) == 0 {
						b2 = "HEAD"
					}
					log.Fatalf("Package %s - cannot switch to %s branch. Branch %s has already been configured", v.Name, b1, b2)
				}
				found = true
				break
			}
		}
		if !found {
			//add new package
			d := new(PacUnit)
			d.Name = p.Depends[i].Name
			d.Uri = p.Depends[i].Uri
			d.Branch = p.Depends[i].Branch
			all_packs = append(all_packs, d)
			fetch(d)
			os.Chdir(cwd)
			p.Depends[i].pack = d
		} else {
			p.Depends[i].pack = all_packs[idx]
			Verbosef("Package %s has already been configured\n", p.Depends[i].Name)
		}
	}

	incdir := filepath.Join(cwd, "include")
	if os.Chdir(incdir) != nil {
		os.Mkdir(incdir, 0755)
		os.Chdir(incdir)
	}

	//create symlinks to dependents
	for _, dep := range p.Depends {
		var target string
		if len(dep.Modules) != 0 {
			for _, m := range dep.Modules {
				target = filepath.Join(devroot, dep.Name, "include", m)
				Verbosef("In '%s' - creating symlink %s --> %s\n", cwd, target, m)
				Symlink(target, m)
			}
		} else {
			target = filepath.Join(devroot, dep.Name, "include", dep.Name)
			Verbosef("In '%s' - creating symlink %s --> %[3]s\n", cwd, target, dep.Name)
			Symlink(target, dep.Name)
		}
	}
	os.Chdir(cwd)
}

// Build a package after first having built its dependents
func build(p *PacUnit) {
	if p.built {
		Verboseln("Package", p.Name, "has already been built")
		return
	}
	for _, w := range inprocess {
		if w == p.Name {
			log.Fatalf("Package %s depends on itself.\n Dependency chain: %v\n", p.Name, inprocess)
		}
	}

	//keep track of packages that are in process to avoid dependency cycles
	inprocess = append(inprocess, p.Name)
	pacdir := filepath.Join(devroot, p.Name)
	os.Chdir(pacdir) //that should be ok. Package has been fetched already
	cwd, _ := os.Getwd()
	Verbosef("Building %s in %s \n", p.Name, cwd)

	//First, build all dependent packages
	if p.Depends != nil {
		var d DependencyDescriptor
		for _, d = range p.Depends {
			if !d.FetchOnly {
				build(d.pack)
				if len(d.Post) != 0 {
					Verboseln("Executing post commands...")
					if ret, err := exec_commands(d.Post); ret != 0 {
						log.Fatalf("Build aborted - %v\n", err)
					}
					Verboseln("...finished post commands")
				}
			} else {
				Verbosef("Package %s - skipped build\n", d.Name)
			}
		}
		os.Chdir(cwd)
	}

	// then build self
	if len(p.Build) != 0 {
		if ret, err := exec_commands(p.Build); ret != 0 {
			log.Fatalf("Build aborted - %v\n", err)
		}
	} else {
		Verboseln("No build command found!")
	}

	inprocess = inprocess[:len(inprocess)-1]
	p.built = true
}

/*
Execute a list of commands.

Executes only commands that apply to current OS environment or generic ones
(os set to "any" or "")
*/
func exec_commands(commands []Command) (int, error) {
	var ret int
	var err error

	for _, c := range commands {
		if c.Os == "" {
			c.Os = "any"
		}
		oses := strings.Fields(c.Os)
		for _, an_os := range oses {
			if an_os == "any" || an_os == runtime.GOOS {
				var exparg []string
				for _, a := range c.Args {
					exparg = append(exparg, os.ExpandEnv(a))
				}
				Verbosef("OS: %s cmd: %s %v\n", an_os, c.Cmd, exparg)
				if ret, err = Run(c.Cmd, exparg, nil); ret != 0 {
					return ret, err
				}
			}
		}
	}
	return ret, err
}

// Builtin CMD commands executed by spawning a CMD instance
var cmd_builtins = [...]string{"attrib", "copy", "del", "echo", "md", "mkdir", "mklink", "rd", "ren", "rename", "replace", "rmdir"}

/*
Run a program with arguments.
GO 1.19 doesn't allow relative paths. Here however we allow those.
*/
func Run(prog string, args []string, out io.Writer) (int, error) {

	if runtime.GOOS == "windows" && slices.Contains(cmd_builtins[:], strings.ToLower(prog)) {
		args = slices.Insert(args, 0, "/c")
		args = slices.Insert(args, 1, prog)
		prog = "cmd"
	}
	cmd := exec.Command(prog, args...)
	if errors.Is(cmd.Err, exec.ErrDot) && runtime.GOOS == "windows" {
		cmd.Err = nil
	}
	if out != nil {
		cmd.Stdout = out
	} else {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return -1, err
	}
	return cmd.ProcessState.ExitCode(), nil
}

func git_current_branch () string {
	var buf bytes.Buffer

	args := []string  {"branch", "--show-current"}
	Run ("git", args, &buf)
	s := buf.String()
	return strings.TrimSpace(s)
}

// Clone a repo
func git_clone_package(p *PacUnit) {
	fullpath := filepath.Join(devroot, p.Name)
	Verbosef("Cloning: %s in %s\n", p.Name, fullpath)

  var uri = p.Uri
	if uri == "" {
		log.Fatalf("Package %s - Missing package location!", p.Name)
	}
	git_clone (uri, p.Branch, fullpath)
}

// Clone a repo in the given directory
func git_clone (uri string, branch string, dir string) {
	//Build git command
	var args []string;
	args = append (args, "clone")
	if branch != "" {
		args = append(args, "-b", branch)
	}
	args = append(args, uri, dir)
	Verboseln("git ", args)

	//Clone
	if stat, err := Run("git", args, nil); err != nil || stat != 0 {
		log.Fatalf("Cloning failed \nStatus %d Error: %v\n", stat, err)
	}
}

// Pull latest version from origin.
func git_pull_package(p *PacUnit) {
	rdir := filepath.Join(devroot, p.Name)
	if err := os.Chdir(rdir); err != nil {
		log.Fatalf("Cannot enter directory %s - %v", rdir, err)
	}

	git_pull (p.Branch)
}

// Pull repo in the current directory from its origin repo
// If branch is not empty, switches to that branch, if needed
func git_pull (branch string) {

	if branch != "" && git_current_branch() != branch {
		git_switch(branch)
	}

	var orig strings.Builder
	Run ("git", []string{"config", "--get", "remote.origin.url"}, &orig)
	Verbosef("Pulling from %s", orig.String())

	if stat, err := Run("git", []string{ "pull", "origin"}, nil); err != nil || stat != 0 {
		log.Fatalf("Pulling failed \nStatus %d Error: %v\n", stat, err)
	}
}

// Return `true` if directory is a Git repo
func git_is_repo (dir string) bool {
	crt_dir,_ := os.Getwd() //save current directory
	if err := os.Chdir(dir); err != nil {
		log.Fatalf("Cannot enter directory %s - %v", dir, err)
	}
	var ret bool = true
	if _, err := os.Stat(filepath.Join(dir, ".git")); os.IsNotExist(err) {
		ret = false;
	}
	os.Chdir(crt_dir) //go back to previous directory
	return ret;
}

func git_switch(branch string) {
	var args []string

	args = append(args, "switch")
	if *force_flag {
		args = append(args, "-f")
	}
	args = append(args, branch)
	Verboseln("Running git ", args)
	if stat, err := Run("git", args, nil); err != nil || stat != 0 {
		log.Fatalf("Switching to branch %s failed \nStatus %d Error: %v\n", *branch_flag, stat, err)
	}
}

// If verbose flag is set, print arguments using default format followed by newline
func Verboseln(s ...interface{}) {
	if *verbose_flag {
		fmt.Println(s...)
	}
}

// If verbose flag is set, print arguments using Printf
func Verbosef(f string, a ...interface{}) {
	if *verbose_flag {
		fmt.Printf(f, a...)
	}
}

// Create symbolic link
//
//	target - destination
//	link   - symlink name
func Symlink(target string, link string) {
	wd, _ := os.Getwd()

	if _, err := os.Stat(link); os.IsNotExist(err) {
		err = os.Symlink(target, link)
		if err != nil {
			le := err.(*os.LinkError)
			log.Fatalf("Fatal - In '%s' - cannot create symlink %s <---> %s", wd, le.Old, le.New)
		}
	} else {
		link_stat, _ := os.Lstat(link)
		tgt_stat, _ := os.Stat(target)
		if link_stat.Mode()&fs.ModeSymlink == 0 {
			log.Fatalf("Fatal - In '%s' - '%s' already exists and is not a symlink to '%s'", wd, link, target)
		}
		link_stat, _ = os.Stat(link)
		if !os.SameFile(link_stat, tgt_stat) {
			log.Fatalf("Fatal - In '%s' - '%s' already exists and is not a symlink to '%s'", wd, link, target)
		}

		Verbosef("In '%s' - link already exists '%s' <---> '%s\n", wd, link, target)
	}
}

// Return `true` if a directory is empty
func IsEmptyDir(name string) (bool, error) {
    f, err := os.Open(name)
    if err != nil {
        return false, err
    }
    defer f.Close()

    _, err = f.Readdirnames(1)
    if err == io.EOF {
        return true, nil
    }
    return false, err // Either not empty or error, suits both cases
}
