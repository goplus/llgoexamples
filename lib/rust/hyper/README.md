## Hyper Wrapper

This repository provides a C language interface for [Hyper](https://github.com/hyperium/hyper) which is a modern embedded database.

### Clone the Repository

First, clone this repository to your local machine:
```SH
git clone --branch v1.4.1 --single-branch https://github.com/hyperium/hyper.git
cd hyper
```
### Build the Project

Build the project using `Cargo` to generate the dynamic library:
```SH
RUSTFLAGS="--cfg hyper_unstable_ffi" cargo rustc --features client,http1,http2,ffi --crate-type cdylib
```
Use a shell script to generate the header file, and the configuration for generating the header file is in cbindgen.toml:
```SH
./capi/gen_header.sh 
```
Then you will see the dynamic library in the `target/debug` directory and the header file in the `capi/include` directory.

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
sudo dylib_installer ./target/debug/ ./capi/include/
```

### Check the Installation

You can check the installation by running the following command:

```SH
pkg-config --libs --cflags hyper
```

if everything is installed correctly, you will see the output like this (depending on your system):

```SH
-I/usr/local/include/hyper -L/usr/local/lib -lhyper
```

### Run hyper's llgo demo

Make sure you have installed the LLGO environment.
```SH
# Enter the file directory where the demo is located. 
# eg:
cd llgoexamples/rust/hyper/_demo/hyper_client
llgo run hyper.go
```