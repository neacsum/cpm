# CPM - A C/C++ Package Manager

CPM is a tool that helps coordinate work between multiple repositories. You need just a simple JSON file to describe dependencies between repositories. This together with the magic of symbolic links helps you maintain a consistent environment for multiple libraries.

## Example ##
You have two libraries `cool_A` and `cool_B` that need to be used in an application `super_App`. Both `cool_A` and `cool_B` use code from another library `utils`. Each one for these has its own Git repository.

You need to create a JSON file called `cpm.json` in each repository:

In `super_App`:
````JSON
{ "name": "super_App", "git": "git@github.com:user/super_App.git",
  "depends": [
      {"name": "cool_A", "git": "git@github.com:user/cool_A.git"},
      {"name": "cool_B", "git": "git@github.com:user/cool_B.git"}
  ],
  "build" : [
      {"os": "windows", "command": "msbuild", "args": ["super_app.proj"]},
      {"os": "linux", "command": "cmake"}
  ]
}
````
In `cool_A`:
````JSON
{ "name": "cool_A", "git": "git@github.com:user/cool_A.git",
  "depends": [
      {"name": "utils", "git": "git@github.com:user/utils.git"},
  ],
  "build": [
      {"os": "windows", "command": "msbuild", "args": ["cool_a.proj"]},
      {"os": "linux", "command": "cmake"}
  ]
}
````

In `cool_B`:
````JSON
{ "name": "cool_B", "git": "git@github.com:user/cool_B.git",
  "depends": [
      {"name": "utils", "git": "git@github.com:user/utils.git"},
  ],
  "build": [
      {"os": "windows", "command": "msbuild", "args": ["cool_b.proj"]},
      {"os": "linux", "command": "cmake"}
  ]
}
````

In `utils`:
````JSON
{ "name": "utils", "git": "git@github.com:user/utils.git",
  "build": [
      {"os": "windows", "command": "msbuild", "args": ["utils.proj"]},
      {"os": "linux", "command": "cmake"}
  ]
}
````

After that, you just have to fetch the `super_App` repository and invoke the CPM utility:
````
cpm super_app
````
It will take care of pulling all the other repositories and issuing the build commands for the current operating system.

## Basic rules ##
To be able to use this magic you have to adhere to a set of basic rules:
> **RULE 1** - All projects have their own folder and all project folders are in one parent folder. The environment variable `DEV_ROOT` points to this root of development tree.

Here is some ASCII art showing the general code layout:
````
DevTreeRoot
   |
   +-- cool_A
   |    |
   |    +-- include
   |    |      |
   |    |      +-- cool_A
   |    |            |
   |    |            +-- hdr1.h
   |    |            |
   |    |            +-- hdr2.h
   |    +-- src
   |    |    |
   |    |    +-- file1.cpp
   |    |    |
   |    |    +-- file2.cpp
   |    +-- project file (cool_A.vcxproj) and other stuff
   |
   +-- cool_B
        |
        +-- include
        |      |
        |      +-- cool_B
        |            |
        |            +-- hdr1.h
        |            |
        |            +-- hdr4.h
        +-- src
        |    |
        |    +-- file1.cpp
        |    |
        |    +-- file2.cpp
        |
        +-- project file (cool_B.vcxproj) and other stuff
````
> **RULE 2** - Include files that need to be visible to users are placed in a subfolder of the `include` folder. The subfolder has the same name as the library.

If users of `cool_A` can refer to `hdr1.h` file like this:
````C
#include <cool_A/hdr1.h>
````
An additional advantage of this organization is that it prevents name clashes between  different libraries. In this case, if a program uses both `cool_A` and `cool_B`, the corresponding include directives will be:
````C
#include <cool_A/hdr1.h>
#include <cool_B/hdr1.h>
````

> **RULE 3** - Include folders of dependent modules are made visible through symbolic links

In the structure shown before, the application that uses `cool_A` and `cool_B` will have an `include` folder but in this folder there are _symbolic links_ to `cool_A` and `cool_B` include folders. The folder structure will look something like this (angle brackets denote symbolic links):
````
DevTreeRoot
  |
  +-- SuperApp
  |      |
  |      +-- include
  |      |     |
  |      |     +-- <cool_A>
  |      |     |      |
  |      |     |      +-- hdr1.h
  |      |     |      |
  |      |     |      +-- hdr2.h
  |      |     |
  |      |     +-- <cool_B>
  |      |     |      |
  |      |     |      +-- hdr1.h
  |      |     |      |
  |      |     |      +-- hdr4.h
  |      |     other header files
  |      |
  |      +-- src
  |      |    |
  |      |    +-- source files
  |      other files
  ...
````
> **RULE 4** - All libraries reside in a `lib` folder at the root of development tree. Each module contains a _symbolic link_ to this folder.

Without repeating the parts already shown of the files layout, here is the part related to `lib` folder (again, angle brackets denote symbolic links):
````
DevTreeRoot
  |
  +-- cool_A
  |     |
  |    ...
  |     +-- <lib>
  |           |
  |           all link libraries are here
  +-- cool_B
  |     |
  |    ...
  |     +-- <lib>
  |           |
  |           all link libraries are here
  +-- SuperApp
  |      |
  |     ...
  |      +-- <lib>
  |            |
  |            all link libraries are here
  +-- lib
       |
       all link libraries are here
````
If there are different flavors of link libraries (debug, release, 32-bit, 64-bit) they can be accommodated as subfolders of the `lib` folder.

## CPM Installation ##
CPM is written in Go. You can download a prebuilt version or you can build it from source. To build it, you need to have the Go compiler [installed](https://go.dev/doc/install). Use the following command to build the executable:
````
go build cpm.go
````

## CPM Usage ##
````
cpm [options] [project]
````
or
````
cpm version
````

If project is not specified, it is assumed to be the current directory.

Valid options are:
  - `-b <branch_name>` switches to a specific branch
  - `-F` discards local changes when switching branches (issues a `git switch -f ...` command)
  - `-f` fetch-only (no build)
  - `-l` local-only (no pull)
  - `-r <folder>` set root of development tree, overriding `DEV_ROOT` environment variable
  - `-v` verbose

## Semantics of CPM.JSON file ##
|Level | Attribute   | Value  | Semantics |
|------|-------------|--------|-----------|
| 1    | `name`      | string | Name of package |
| 1    | `git`       | string | Download location for the package |
| 1    | `build`     | array  | Commands to be issued for building the package. |
| 2    | `os`        | string | OS to which the build command applies |
| 2    | `command`   | string | Command issued for building the package |
| 2    | `args`      | array  | Command arguments |
| 1    | `depends`   | array  | Package dependencies |
| 2    | `name`      | string | Name of dependency |
| 2    | `git`       | string | Download location for dependency |
| 2    | `fetchOnly` | bool   | Weak dependency (see below) |

## Operation ##
CPM reads the CPM.JSON file in the selected folder and, for each dependent package, it checks if the project folder exists. If not, it issues a `git clone` command to bring the latest version. If you have selected a specific branch, CPM issues a `git switch ...` command to switch to that branch and then a `git pull ...` command to bring in the latest version of that branch.

The next step is to build build each package by issuing the build commands appropriate for the OS environment. All commands that have an `os` attribute matching the current OS or without any `os` attribute are issued in order.

## Weak Dependencies ##
Sometimes it may happen that two modules are interdependent. For instance `cool_A` needs a type definition that is provided by `cool_B`. Symbolic links can take care of this situation like shown below:
````
DevTreeRoot
   |
   +-- cool_A
   |    |
   |    +-- include
   |           |
   |           +-- cool_A
   |           |     |
   |           |     +-- hdr1.h
   |           |     |
   |           |     +-- hdr2.h
   |           |
   |           +--<cool_B>
   |                 |
   |                 +-- hdr1.h
   |                 |
   |                 +-- hdr2.h
   +-- cool_B
        |
        +-- include
               |
               +-- cool_B
               |     |
               |     +-- hdr1.h
               |     |
               |     +-- hdr4.h
               |
               +--<cool_A>
                     |
                     +-- hdr1.h
                     |
                     +-- hdr2.h
````
In such cases, CPM has to fetch the packages and create the symbolic links but should not initiate the build process of `cool_B` as part of the build process for `cool_A`. These situations are called *weak dependencies* and are flagged by the `fetchOnly` flag in the CPM.JSON file.
