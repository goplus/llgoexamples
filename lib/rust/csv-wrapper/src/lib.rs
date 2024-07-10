extern crate libc;
extern crate csv;

use libc::c_char;
use std::ffi::{CStr, CString};
use std::ptr;
use std::os::raw::c_void;
use std::fs::File;

/// Create a new CSV reader for the specified file path.
#[no_mangle]
pub extern "C" fn csv_reader_new(file_path: *const c_char) -> *mut c_void {
    let file_path = unsafe {
        if file_path.is_null() { return ptr::null_mut(); }
        match CStr::from_ptr(file_path).to_str() {
            Ok(s) => s,
            Err(_) => return ptr::null_mut(),
        }
    };

    let reader = csv::ReaderBuilder::new().from_path(file_path);
    match reader {
        Ok(r) => Box::into_raw(Box::new(r)) as *mut c_void,
        Err(_) => ptr::null_mut(),
    }
}

/// Free the memory allocated for the CSV reader.
#[no_mangle]
pub extern "C" fn csv_reader_free(ptr: *mut c_void) {
    if !ptr.is_null() {
        let reader: Box<csv::Reader<File>> = unsafe { Box::from_raw(ptr as *mut csv::Reader<File>) };
        std::mem::drop(reader);
    }
}

/// Read the next record from the CSV reader and return it as a C string.
#[no_mangle]
pub extern "C" fn csv_reader_read_record(ptr: *mut c_void) -> *const c_char {
    let reader = unsafe {
        assert!(!ptr.is_null());
        &mut *(ptr as *mut csv::Reader<File>)
    };

    let mut record = csv::StringRecord::new();
    match reader.read_record(&mut record) {
        Ok(true) => match CString::new(format!("{:?}\n", record)) {
            Ok(c_string) => c_string.into_raw(),
            Err(_) => ptr::null(),
        },
        _ => ptr::null(),
    }
}

/// Free the memory allocated for a C string returned by other functions.
#[no_mangle]
pub extern "C" fn free_string(s: *mut c_char) {
    if s.is_null() {
        return;
    }
    unsafe {
        let c_string = CString::from_raw(s);
        std::mem::drop(c_string);
    }
}
