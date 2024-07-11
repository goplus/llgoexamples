use std::env;

fn main() {
    let crate_dir = env::var("CARGO_MANIFEST_DIR").unwrap();

    // Load the configuration file
    let config = cbindgen::Config::from_file("cbindgen.toml")
        .expect("Unable to find cbindgen.toml configuration file");

    // Tell cargo to rerun this build script if the wrapper changes
    println!("cargo:rerun-if-changed=include/sled.h");

    // Generate the C header file
    cbindgen::generate_with_config(&crate_dir, config)
        .unwrap()
        .write_to_file("include/sled.h");
}