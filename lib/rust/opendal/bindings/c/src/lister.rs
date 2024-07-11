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

use ::opendal as core;

use super::*;

/// \brief BlockingLister is designed to list entries at given path in a blocking
/// manner.
///
/// Users can construct Lister by `blocking_list` or `blocking_scan`(currently not supported in C binding)
///
/// For examples, please see the comment section of opendal_operator_list()
/// @see opendal_operator_list()
#[repr(C)]
pub struct opendal_lister {
    inner: *mut core::BlockingLister,
}

impl opendal_lister {
    pub(crate) fn new(lister: core::BlockingLister) -> Self {
        Self {
            inner: Box::into_raw(Box::new(lister)),
        }
    }

    /// \brief Return the next object to be listed
    ///
    /// Lister is an iterator of the objects under its path, this method is the same as
    /// calling next() on the iterator
    ///
    /// For examples, please see the comment section of opendal_operator_list()
    /// @see opendal_operator_list()
    #[no_mangle]
    pub unsafe extern "C" fn opendal_lister_next(&self) -> opendal_result_lister_next {
        let e = (*self.inner).next();
        if e.is_none() {
            return opendal_result_lister_next {
                entry: std::ptr::null_mut(),
                error: std::ptr::null_mut(),
            };
        }

        match e.unwrap() {
            Ok(e) => {
                let ent = Box::into_raw(Box::new(opendal_entry::new(e)));
                opendal_result_lister_next {
                    entry: ent,
                    error: std::ptr::null_mut(),
                }
            }
            Err(e) => opendal_result_lister_next {
                entry: std::ptr::null_mut(),
                error: opendal_error::new(e),
            },
        }
    }

    /// \brief Free the heap-allocated metadata used by opendal_lister
    #[no_mangle]
    pub unsafe extern "C" fn opendal_lister_free(p: *const opendal_lister) {
        unsafe {
            let _ = Box::from_raw((*p).inner);
            let _ = Box::from_raw(p as *mut opendal_lister);
        }
    }
}
