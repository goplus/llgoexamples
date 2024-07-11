# Sled Wrapper

This repository provides a C language interface for [Sled](https://github.com/spacejam/sled) which is a modern embedded database.

### Clone the Repository

First, clone this repository to your local machine:

```
git clone https://github.com/goplus/llgoexamples
cd lib/rust/sled
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

```bash
brew tap hackerchai/tap
brew install dylib-installer
```

Or you can install it using Cargo:

```bash 
cargo install dylib_installer
```

### Install Dynamic Library

Use dylib-installer to install the built dynamic library and the header file into the system directory:

```
sudo dylib_installer ./target/release/ ./include/
```

### Check the Installation

You can check the installation by running the following command:

```
pkg-config --libs --cflags sled
```

if everything is installed correctly, you will see the output like this (depending on your system):

```
-I/usr/local/include/sled -L/usr/local/lib -lsled
```
