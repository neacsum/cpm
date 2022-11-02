package main

/*
  CPM - C/C++ Package Manager
  (c) Mircea Neacsu 2021-2022

  This tool uses simple JSON files to manage dependencies between
  different packages.

  Usage:
    cpm [options] [<package>]
    or
      cpm version

  If package name is missing, the program assumes to be the current
  directory.

  Valid options are:
    -b <branch name> switches to specific branch
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
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const Version = "V0.5.1"

type BuildCommands struct {
	Os   string
	Cmd  string
	Args []string
}

type DependencyDescriptor struct {
	Name      string
	Git       string
	Https     string
	Modules   []string
	FetchOnly bool
	pack      *PacUnit
}

type PacUnit struct {
	Name    string
	Git     string
	Https   string
	Build   []BuildCommands
	Depends []DependencyDescriptor
	built   bool
}

var devroot string         //root of development tree
var root_descriptor string //path of root module
const descriptor_name = "cpm.json"

var all_packs []*PacUnit

var inprocess []string
var root_uri string

// command line flags
var force_flag = flag.Bool("F", false, "discard local changes")
var fetch_flag = flag.Bool("f", false, "fetch only (no build)")
var local_flag = flag.Bool("l", false, "local only (no pull)")
var verbose_flag = flag.Bool("v", false, "verbose")
var branch_flag = flag.String("b", "", "select branch")
var proto_flag = flag.String("proto", "git", "download protocol")

func main() {
	var err error
	var show_ver bool

	println("C/C++ Package Manager " + Version)
	flag.StringVar(&root_uri, "uri", "", "root URI")
	flag.StringVar(&root_uri, "u", "", "root URI")
	flag.StringVar(&devroot, "r", os.Getenv("DEV_ROOT"), "development tree root")
	flag.StringVar(&devroot, "root", os.Getenv("DEV_ROOT"), "development tree root")
	flag.BoolVar(&show_ver, "version", false, "show version")
	start := time.Now()
	flag.Usage = func() {
		println(`Usage: cpm [options] [package]
        
  If package is not specified, it is assumed to be the current directory.
  Valid options are:
    -b <branch name>          checkout specific branch
    -f                        fetch-only (no build)
    -l                        local-only (no pull)
    -r <folder>               set root of development tree
    --uri <uri> (or -u <uri>) URI of root package
    --proto [git|https]       preferred download protocol
    -v                        verbose
    --help (or -h)            prints this message`)
	}

	flag.Parse()
	if show_ver || (flag.NFlag() == 0 && flag.NArg() > 0 && flag.Arg(0) == "version") {
		os.Exit(0)
	}

	if (*proto_flag != "git") && (*proto_flag != "https") {
		log.Fatal("Unknown protocol. Must be 'git' or 'https'")
	}

	if devroot == "" {
		log.Fatal("No development tree root specified and environment variable  DEV_ROOT is not set")
	}
	devroot, _ = filepath.Abs(devroot)

	var root_name string
	if flag.NArg() > 0 {
		dir := ""
		//root package specified on command line
		if !strings.ContainsAny(flag.Arg(0), "\\/") {
			//only a relative path - use DEVROOT as path
			root_name = flag.Arg(0)
			dir = devroot
		} else {
			_, root_name = filepath.Split(flag.Arg(0))
		}
		root_descriptor = filepath.Join(dir, root_name, descriptor_name)
	} else {
		//assume root package is in current folder
		cwd, _ := os.Getwd()
		_, root_name = filepath.Split(cwd)
		root_descriptor = filepath.Join(cwd, descriptor_name)
	}

	Verboseln("DEV_ROOT=", devroot)
	Verboseln("Top descriptor is ", root_descriptor)
	os.Mkdir(filepath.Join(devroot, "lib"), 0755)

	root := new(PacUnit)
	all_packs = append(all_packs, root)

	if root_uri != "" {
		//fetch root package
		root.Git = root_uri
		root.Name = root_name
		fetch(root)
	}

	var data []byte
	if data, err = os.ReadFile(root_descriptor); err != nil {
		log.Fatalf("cannot open '%s' file", root_descriptor)
	}

	if err = json.Unmarshal(data, root); err != nil {
		log.Fatalf("cannot parse %s - %v\n", root_descriptor, err)
	}

	if root_name != "" && !strings.EqualFold(root.Name, root_name) {
		fmt.Printf("WARNING specifed package directory '%s' does not match descriptor's package name (%s)\n", root_name, root.Name)
		root.Name = root_name
	}
	os.Chdir(filepath.Join(devroot, root.Name))

	cwd, _ := os.Getwd()
	Verboseln("Changed directory to", cwd)

	fetch_all(root)

	if !*fetch_flag {
		inprocess = make([]string, 0, 10)
		build(root)
	}

	fmt.Println("CPM operation finished in", time.Since(start).Round(100*time.Microsecond))
}

// Fetch one package. Changes working directory to the package directory
func fetch(p *PacUnit) {
	pacdir := filepath.Join(devroot, p.Name)

	if _, err := os.Stat(pacdir); os.IsNotExist(err) {
		//package directory doesn't exist; create it and clone repo
		if *local_flag {
			log.Fatalf("Fatal - local-only mode and %s does not exist", p.Name)
		}
		if err := os.Mkdir(pacdir, 0666); err != nil {
			log.Fatalf("error %d - cannot create folder %s", err, pacdir)
		}
		git_clone(p)
		os.Chdir(pacdir)
	} else if _, err := os.Stat(filepath.Join(pacdir, ".git")); os.IsNotExist(err) {
		//package directory exists but no git repo here; clone repo
		git_clone(p)
		os.Chdir(pacdir)
	} else {
		//repo exists; just pull latest version
		os.Chdir(pacdir)
		if !*local_flag {
			git_pull(pacdir)
		}
	}
}

// Fetch a package and all its dependents
func fetch_all(p *PacUnit) {

	fetch(p) //fetch top package
	cwd, _ := os.Getwd()
	Verbosef("Setting up %s in %s\n", p.Name, cwd)

	os.Symlink(devroot+"lib", "lib")

	data, err := os.ReadFile(descriptor_name)
	if err != nil {
		Verbosef(" %s\\%s file not found. Assuming no dependencies\n", cwd, descriptor_name)
	} else {
		if err = json.Unmarshal(data, &p); err != nil {
			log.Fatalf("cannot parse %s - %v", descriptor_name, err)
		}
	}

	if p.Depends != nil {
		//setup all dependent packages
		for i := range p.Depends {
			var v *PacUnit
			var idx int

			//search if already setup
			found := false
			for idx, v = range all_packs {
				if v.Name == p.Depends[i].Name {
					found = true
					break
				}
			}
			if !found {
				//add new package
				d := new(PacUnit)
				all_packs = append(all_packs, d)
				d.Name = p.Depends[i].Name
				d.Git = p.Depends[i].Git
				d.Https = p.Depends[i].Https
				fetch_all(d)
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
					Verbosef("In %s creating symlink %s --> %s\n", cwd, target, m)
					os.Symlink(target, m)
				}
			} else {
				target = filepath.Join(devroot, dep.Name, "include", dep.Name)
				Verbosef("In %s creating symlink %s --> %[3]s\n", cwd, target, dep.Name)
				os.Symlink(target, dep.Name)
			}
		}
	}
}

// Build a packge after first having built its dependents
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

	//keep track of packeges that are in process to avoid dependency cycles
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
			} else {
				Verbosef("Package %s - skipped build\n", d.Name)
			}
		}
		os.Chdir(cwd)
	}

	// then build self
	if ret, err := do_build(p.Build); ret != 0 {
		log.Fatalf("Build aborted - %v\n", err)
	}
	inprocess = inprocess[:len(inprocess)-1]
	p.built = true
}

/*
Execute the appropriate build command for a package. If there is a specific
command for the current OS envirnoment, use that one. Otherwise choose a
generic one (os set to "any" or "")
*/
func do_build(commands []BuildCommands) (int, error) {
	var ret int
	var err error

	if len(commands) == 0 {
		Verboseln("No build command found!")
		return 0, nil
	}
	for _, c := range commands {
		if len(c.Os) == 0 || c.Os == "any" || (len(c.Os) > 0 && c.Os == runtime.GOOS) {
			Verbosef("OS: %s cmd: %s %v\n", c.Os, c.Cmd, c.Args)
			if ret, err = Run(c.Cmd, c.Args); ret != 0 {
				return ret, err
			}
		}
	}
	return ret, err
}

func Run(prog string, args []string) (int, error) {
	cmd := exec.Command(prog, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return -1, err
	}
	return cmd.ProcessState.ExitCode(), nil
}

// Clone a repo
func git_clone(p *PacUnit) {
	fullpath := filepath.Join(devroot, p.Name)
	Verbosef("Cloning: %s in %s\n", p.Name, fullpath)

	// Find URL for cloning
	var uri string
	if *proto_flag == "https" {
		if p.Https == "" {
			Verboseln("  -- missing https URI")
			uri = p.Git
		} else {
			uri = p.Https
		}
	} else {
		if p.Git == "" {
			Verboseln("  -- missing git URI")
			uri = p.Https
		} else {
			uri = p.Git
		}
	}
	if uri == "" {
		log.Fatal("Missing package location.")
	}

	//Build git command
	var args []string
	args = append(args, "clone")
	if *branch_flag != "" {
		args = append(args, "-b", *branch_flag)
	}
	args = append(args, uri, fullpath)
	Verboseln("git ", args)

	//Clone
	if stat, err := Run("git", args); err != nil || stat != 0 {
		log.Fatalf("Cloning failed \nStatus %d Error: %v\n", stat, err)
	}
}

// Pull latest version from repo.
// If branch is not empty, stwitches to that branch
func git_pull(dir string) {
	var args []string

	if len(*branch_flag) != 0 {
		Verbosef("In %s - Switching to: %s\n", dir, *branch_flag)
		args = append(args, "switch")
		if *force_flag {
			args = append(args, "-f")
		}
		args = append(args, *branch_flag)
		Verboseln("In", dir, "- git", args)
		if stat, err := Run("git", args); err != nil || stat != 0 {
			log.Fatalf("Switching to branch %s failed \nStatus %d Error: %v\n", *branch_flag, stat, err)
		}
	}
	args = nil
	args = append(args, "pull", "origin")
	args = append(args, *branch_flag)
	Verboseln("In", dir, "- git ", args)
	if stat, err := Run("git", args); err != nil || stat != 0 {
		log.Fatalf("Pulling failed \nStatus %d Error: %v\n", stat, err)
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
