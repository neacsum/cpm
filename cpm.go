package main

/*
	CPM - C/C++ Package Manager
	(c) Mircea Neacsu 2021

	This tool uses simple JSON files to manage dependencies between
	different projects.

	Usage:
		cpm [options] [<project>]

	If project neame is missing, the program assumes to be the current
	directory.

	Valid options are:
		-f fetch-only (do not build)
		-v verbose

	The program opens the '${DEV_ROOT}/<project>/cpm.json' file and
	recursively searches for all dependencies.

	${DEV_ROOT} environment variable is the root of development tree.

*/
import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"

	"github.com/go-git/go-git/v5"
)

type PacUnit struct {
	Name    string
	Git     string
	Build   string
	Depends []*PacUnit
}

var devroot string         //root of development tree
var root_descriptor string //path of root module
const descriptor_name = "cpm.json"

var seen map[string]bool
var inprocess []string

//command line flags
var fetch_flag, verbose_flag *bool

func main() {
	var err error
	fetch_flag = flag.Bool("f", false, "fetch only (no build)")
	verbose_flag = flag.Bool("v", false, "verbose")

	if devroot = os.Getenv("DEV_ROOT"); len(devroot) == 0 {
		log.Fatal("Environment variable  DEV_ROOT not set")
	}
	//make sure DEV_ROOT is terminated with a path separator
	if devroot[len(devroot)-1] != '/' && devroot[len(devroot)-1] != '\\' {
		devroot += "/"
	}
	flag.Usage = func() {
		println(`C/C++ Package Manager
  Usage: cpm [options] [project]
				
  If project is not specified, it is assumed to be the current directory.
  Valid options are:
    -f fetch-only (no build)
    -v verbose
    -h help - prints this message`)
	}

	flag.Parse()

	if flag.NArg() > 0 {
		root_descriptor = devroot + flag.Arg(0) + "/" + descriptor_name
	} else {
		cwd, _ := os.Getwd()
		root_descriptor = cwd + "/" + descriptor_name
	}

	Verboseln("DEV_ROOT = " + devroot)

	seen = make(map[string]bool)
	var root PacUnit
	var data []byte
	if data, err = os.ReadFile(root_descriptor); err != nil {
		log.Fatalf("cannot open '%s' file", root_descriptor)
	}

	json.Unmarshal(data, &root)

	os.Chdir(devroot + root.Name)

	cwd, _ := os.Getwd()
	Verboseln("Changed directory to " + cwd)

	inprocess = make([]string, 0, 10)
	fetch(root)
}

// Fetch a package and all its dependents
func fetch(p PacUnit) {
	if seen[p.Name] {
		Verboseln("Package " + p.Name + " has already been configured")
		return
	}
	for _, w := range inprocess {
		if w == p.Name {
			log.Fatalf("Package %s depends on itself.\n Dependency chain: %v", p.Name, inprocess)
		}
	}

	inprocess = append(inprocess, p.Name)
	pacdir := devroot + p.Name
	if os.Chdir(pacdir) != nil {
		if err := os.Mkdir(pacdir, 0666); err != nil {
			log.Fatalf("error %d - cannot create folder %s", err, pacdir)
		}
		clone(p.Git, p.Name)
		os.Chdir(pacdir)
	}

	cwd, _ := os.Getwd()
	Verbosef("Setting up %s in %s \n", p.Name, cwd)

	r, err := git.PlainOpen(".")
	if err != nil {
		log.Fatalf("cannot open repository in %s error %v", cwd, err)
	}

	w, _ := r.Worktree()
	w.Pull(&git.PullOptions{})
	os.Symlink(devroot+"lib", "lib")

	var pp PacUnit
	data, err := os.ReadFile(descriptor_name)
	if err != nil {
		Verbosef(" %s\\%s file not found. Assuming no dependencies\n", cwd, descriptor_name)
		pp.Name = p.Name
	} else {
		json.Unmarshal(data, &pp)
	}
	seen[p.Name] = true

	if pp.Depends != nil {
		//setup all dependent packages
		var d *PacUnit
		for _, d = range pp.Depends {
			fetch(*d)
		}

		if os.Chdir(pacdir+"/include") != nil {
			os.Mkdir(pacdir+"/include", 0755)
			os.Chdir(pacdir + "/include")
		}

		//create symlinks to dependents
		cwd, _ = os.Getwd()
		for _, dep := range pp.Depends {
			Verboseln("Creating symlink " + cwd + "\\" + dep.Name + " --> " + dep.Name)
			os.Symlink(devroot+cwd+"\\"+dep.Name, dep.Name)
		}
	}

	os.Chdir(pacdir)
	if !*fetch_flag {
		if len(p.Build) > 0 {
			Verboseln("Building in " + cwd + " : " + p.Build)
		} else {
			Verboseln("No build command specified for " + p.Name)
		}
	}
	inprocess = inprocess[:len(inprocess)-1]
}

func clone(url, dir string) bool {
	fullpath := devroot + dir
	var attr os.ProcAttr
	attr.Files = []*os.File{os.Stdin, os.Stdout, os.Stderr}
	var args []string
	var arg0 string
	if runtime.GOOS == "windows" {
		args = []string{"/C", "git", "clone", url, fullpath}
		systemRootDir := os.Getenv("SystemRoot")
		arg0 = systemRootDir + "\\System32\\cmd.exe"
	} else {
		args = []string{"git", "clone", url, fullpath}
		arg0 = "git"
	}
	Verbosef("Cloning: % v", args)

	var exe *os.Process
	var err error
	if exe, err = os.StartProcess(arg0, args, &attr); err != nil {
		log.Fatalf("Command % v failed\nError: %v\n", args, err)
	}
	if s, ok := exe.Wait(); ok != nil {
		return s.ExitCode() == 0
	}
	return false
}

func Verboseln(s string) {
	if *verbose_flag {
		fmt.Println(s)
	}
}

func Verbosef(f string, a ...interface{}) {
	if *verbose_flag {
		fmt.Printf(f, a...)
	}
}
