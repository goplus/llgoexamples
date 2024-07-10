## CSV Wrapper
This repository provides a C language interface for a CSV file processing library written in Rust.

### Clone the Repository
First, clone this repository to your local machine:

```
git clone https://github.com/luoliwoshang/csv-wrapper
cd csv-wrapper
```
Build the Project
Build the project using Cargo to generate the dynamic library:

```
cargo build --release
```
### Generate Header File
Generate the C language header file using cbindgen:
```bash
cbindgen --config cbindgen.toml --crate csv_wrapper --output csv_wrapper.h
```
This step will create a header file named csv_wrapper.h based on the Rust code and the configuration specified in cbindgen.toml.

### Install dylib-installer
Install the dylib-installer tool, which is used to install dynamic libraries:

```bash
cargo install --git https://github.com/hackerchai/dylib-installer
```
### Install Dynamic Library
Use dylib-installer to install the built dynamic library into the system directory:
```
sudo dylib_installer -d ./target/release/
```