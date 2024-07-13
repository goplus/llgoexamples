// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

use std::ffi::CString;
use std::os::raw::c_char;

use ::opendal as core;

/// \brief opendal_list_entry is the entry under a path, which is listed from the opendal_lister
///
/// For examples, please see the comment section of opendal_operator_list()
/// @see opendal_operator_list()
/// @see opendal_list_entry_path()
/// @see opendal_list_entry_name()
#[repr(C)]
pub struct opendal_entry {
    inner: *mut core::Entry,
}

impl opendal_entry {
    /// Used to convert the Rust type into C type
    pub(crate) fn new(entry: core::Entry) -> Self {
        Self {
            inner: Box::into_raw(Box::new(entry)),
        }
    }

    /// \brief Path of entry.
    ///
    /// Path is relative to operator's root. Only valid in current operator.
    ///
    /// \note To free the string, you can directly call free()
    #[no_mangle]
    pub unsafe extern "C" fn opendal_entry_path(&self) -> *mut c_char {
        let s = (*self.inner).path();
        let c_str = CString::new(s).unwrap();
        c_str.into_raw()
    }

    /// \brief Name of entry.
    ///
    /// Name is the last segment of path.
    /// If this entry is a dir, `Name` MUST endswith `/`
    /// Otherwise, `Name` MUST NOT endswith `/`.
    ///
    /// \note To free the string, you can directly call free()
    #[no_mangle]
    pub unsafe extern "C" fn opendal_entry_name(&self) -> *mut c_char {
        let s = (*self.inner).name();
        let c_str = CString::new(s).unwrap();
        c_str.into_raw()
    }

    /// \brief Frees the heap memory used by the opendal_list_entry
    #[no_mangle]
    pub unsafe extern "C" fn opendal_entry_free(ptr: *mut opendal_entry) {
        if !ptr.is_null() {
            let _ = unsafe { Box::from_raw((*ptr).inner) };
            let _ = unsafe { Box::from_raw(ptr) };
        }
    }
}
