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

use std::collections::HashMap;
use std::os::raw::c_char;

use opendal::Buffer;

/// \brief opendal_bytes carries raw-bytes with its length
///
/// The opendal_bytes type is a C-compatible substitute for Vec type
/// in Rust, it has to be manually freed. You have to call opendal_bytes_free()
/// to free the heap memory to avoid memory leak.
///
/// @see opendal_bytes_free
#[repr(C)]
pub struct opendal_bytes {
    /// Pointing to the byte array on heap
    pub data: *const u8,
    /// The length of the byte array
    pub len: usize,
}

impl opendal_bytes {
    /// Construct a [`opendal_bytes`] from the Rust [`Vec`] of bytes
    pub(crate) fn new(buf: Buffer) -> Self {
        let vec = buf.to_vec();
        let data = vec.as_ptr();
        let len = vec.len();
        std::mem::forget(vec);
        Self { data, len }
    }

    /// \brief Frees the heap memory used by the opendal_bytes
    #[no_mangle]
    pub unsafe extern "C" fn opendal_bytes_free(bs: *mut opendal_bytes) {
        if !bs.is_null() {
            let data_mut = unsafe { (*bs).data as *mut u8 };
            // free the vector
            let _ = unsafe { Vec::from_raw_parts(data_mut, (*bs).len, (*bs).len) };
            // free the pointer
            let _ = unsafe { Box::from_raw(bs) };
        }
    }
}

#[allow(clippy::from_over_into)]
impl Into<Buffer> for opendal_bytes {
    fn into(self) -> Buffer {
        let slice = unsafe { std::slice::from_raw_parts(self.data, self.len) };
        Buffer::from(bytes::Bytes::copy_from_slice(slice))
    }
}

/// \brief The configuration for the initialization of opendal_operator.
///
/// \note This is also a heap-allocated struct, please free it after you use it
///
/// @see opendal_operator_new has an example of using opendal_operator_options
/// @see opendal_operator_options_new This function construct the operator
/// @see opendal_operator_options_free This function frees the heap memory of the operator
/// @see opendal_operator_options_set This function allow you to set the options
#[repr(C)]
pub struct opendal_operator_options {
    /// The pointer to the Rust HashMap<String, String>
    /// Only touch this on judging whether it is NULL.
    inner: *mut HashMap<String, String>,
}

impl opendal_operator_options {
    /// \brief Construct a heap-allocated opendal_operator_options
    ///
    /// @return An empty opendal_operator_option, which could be set by
    /// opendal_operator_option_set().
    ///
    /// @see opendal_operator_option_set
    #[no_mangle]
    pub extern "C" fn opendal_operator_options_new() -> *mut Self {
        let map: HashMap<String, String> = HashMap::default();
        let options = Self {
            inner: Box::into_raw(Box::new(map)),
        };

        Box::into_raw(Box::new(options))
    }

    /// \brief Set a Key-Value pair inside opendal_operator_options
    ///
    /// # Safety
    ///
    /// This function is unsafe because it dereferences and casts the raw pointers
    /// Make sure the pointer of `key` and `value` point to a valid string.
    ///
    /// # Example
    ///
    /// ```C
    /// opendal_operator_options *options = opendal_operator_options_new();
    /// opendal_operator_options_set(options, "root", "/myroot");
    ///
    /// // .. use your opendal_operator_options
    ///
    /// opendal_operator_options_free(options);
    /// ```
    #[no_mangle]
    pub unsafe extern "C" fn opendal_operator_options_set(
        &mut self,
        key: *const c_char,
        value: *const c_char,
    ) {
        let k = unsafe { std::ffi::CStr::from_ptr(key) }
            .to_str()
            .unwrap()
            .to_string();
        let v = unsafe { std::ffi::CStr::from_ptr(value) }
            .to_str()
            .unwrap()
            .to_string();
        (*self.inner).insert(k, v);
    }

    /// Returns a reference to the underlying [`HashMap<String, String>`]
    pub(crate) fn as_ref(&self) -> &HashMap<String, String> {
        unsafe { &*(self.inner) }
    }

    /// \brief Free the allocated memory used by [`opendal_operator_options`]
    #[no_mangle]
    pub unsafe extern "C" fn opendal_operator_options_free(
        options: *const opendal_operator_options,
    ) {
        let _ = unsafe { Box::from_raw((*options).inner) };
        let _ = unsafe { Box::from_raw(options as *mut opendal_operator_options) };
    }
}
