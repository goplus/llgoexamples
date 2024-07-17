## Hyper Wrapper

This repository provides a C language interface for [Opendal](https://github.com/apache/opendal) which is a modern embedded database.

### Clone the Repository

First, clone this repository to your local machine:
```SH
git clone --branch v0.47.3 --single-branch https://github.com/apache/opendal.git
cd opendal/bindings/c
```
### Build the Project

Build the project using Cargo to generate the dynamic library and the header file:

```
cargo build --release
```

This step will also automatically create a header file named sled.h based on the Rust code and the configuration specified in `cbindgen.toml`.

Then you will see the dynamic library in the `target/release` directory and the header file in the `include` directory.

### Install dylib-installer
Install the [dylib-installer](https://github.com/hackerchai/dylib-installer) tool, which is used to install dynamic libraries:

```SH
brew tap hackerchai/tap
brew install dylib-installer
```

Or you can install it using Cargo:

```SH 
cargo install dylib_installer
```

### Install Dynamic Library

Use dylib-installer to install the built dynamic library and the header file into the system directory:

```SH
sudo dylib_installer ./target/release/ ./include/
```

### Check the Installation

You can check the installation by running the following command:

```SH
pkg-config --libs --cflags opendal_c
```

if everything is installed correctly, you will see the output like this (depending on your system):

```SH
-I/usr/local/include -L/usr/local/lib -lopendal_c
```

### Run hyper's llgo demo

Make sure you have installed the LLGO environment.
```SH
# Enter the file directory where the demo is located. 
# eg:
cd llgoexamples/rust/opendal/_demo/memory_demo
llgo run opendal.go
# llgo run error_handle.go
```