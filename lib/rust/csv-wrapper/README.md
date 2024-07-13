## CSV Wrapper

This repository provides a C language interface for a CSV file processing library written in Rust.

### Clone the Repository

First, clone this repository to your local machine:

```
git clone https://github.com/goplus/llgoexamples
cd lib/rust/csv-wrapper
```

### Build the Project
Build the project using Cargo to generate the dynamic library:

```
cargo build --release
```

### Generate Header File

Generate the C language header file using cbindgen:

```bash
cbindgen --config cbindgen.toml --crate csv_wrapper --output include/csv_wrapper.h
```

This step will create a header file named csv_wrapper.h based on the Rust code and the configuration specified in cbindgen.toml.

### Install dylib-installer

Install the dylib-installer tool, which is used to install dynamic libraries:

```bash
brew tap hackerchai/tap
brew install dylib-installer
```

Or you can install it using Cargo:

```bash 
cargo install dylib_installer
```

### Install Dynamic Library

Use dylib-installer to install the built dynamic library and header file into the system directory:

```
sudo dylib_installer ./target/release/ ./include
```
