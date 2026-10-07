# Grain stdlib fixture notices

These fixtures contain unchanged Array/String and a modified JSON test subset from commit
`49829d7966b38b177291f7e91f5eb81c65ec07aa` and linked Grain runtime/stdlib code.
The source archive retains every original source header. JSON omits the entire
Validation module and adds a dated modification notice; see `JSON_SUBSET.md`. This file repeats the
third-party notices for recipients of the binaries.

Upstream project copyright, retained from the
[pinned README](https://github.com/grain-lang/grain/blob/49829d7966b38b177291f7e91f5eb81c65ec07aa/README.md):

Copyright ©️ 2017-2025 Philip Blair, Oscar Spencer, & contributors.

The three test inputs live under `compiler/test/stdlib`, outside `stdlib/LICENSE`.
They are conservatively distributed under the repository's LGPLv3 terms;
see `COPYING.LESSER` and its incorporated GPLv3 text in `COPYING`.
The linked stdlib/runtime has the MIT notice in `LICENSE.stdlib`, together with
the additional notices below. These files are separate corpus fixtures;
they do not change Wago's own license.

## Elm Array code: BSD 3-Clause

`stdlib/array.gr` attributes its array tree implementation to Elm's Array module.

Copyright 2014-present Evan Czaplicki

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice,
   this list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.

3. Neither the name of the copyright holder nor the names of its
   contributors may be used to endorse or promote products derived from this
   software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE
LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR
CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF
SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS
INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN
CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE
POSSIBILITY OF SUCH DAMAGE.

## Sun Microsystems numerical code

`stdlib/number.gr` and `stdlib/runtime/numbers.gr` retain this notice:

====================================================
Applies to all functions with a comment referring here.

Copyright (C) 2004 by Sun Microsystems, Inc. All rights reserved.

Permission to use, copy, modify, and distribute this
software is freely granted, provided that this notice
is preserved.

====================================================

`stdlib/runtime/math/kernel/{sin,cos,tan}.gr` and `stdlib/runtime/math/rempio2.gr` retain this notice:

====================================================
Copyright (C) 1993 by Sun Microsystems, Inc. All rights reserved.

Developed at SunSoft, a Sun Microsystems, Inc. business.
Permission to use, copy, modify, and distribute this
software is freely granted, provided that this notice
is preserved.
====================================================

## Rust decimal-to-float code: MIT

`stdlib/runtime/atof/{common,decimal,lemire,parse,slow}.gr` attributes its implementation to Rust commit `1cbc45942d5c0f6eb5d94e3b10762ba541958035` and preserves this license text:

Permission is hereby granted, free of charge, to any
person obtaining a copy of this software and associated
documentation files (the "Software"), to deal in the
Software without restriction, including without
limitation the rights to use, copy, modify, merge,
publish, distribute, sublicense, and/or sell copies of
the Software, and to permit persons to whom the Software
is furnished to do so, subject to the following
conditions:

The above copyright notice and this permission notice
shall be included in all copies or substantial portions
of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF
ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED
TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A
PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT
SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY
CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION
OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR
IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER
DEALINGS IN THE SOFTWARE.

## Nim bigint code: MIT

`stdlib/runtime/bigint.gr` retains this attribution for the portions based on `nim-lang/bigints`:

This file is *not* a direct port of `nim-lang/bigints`, but pieces of it are, and it does draw substantial inspiration from it.
The following is the copyright notice from the `nim-lang/bigints` project (MIT License same as license for Grain standard library):

Copyright 2019 Dennis Felsing

The applicable MIT permission terms are included in `LICENSE.stdlib`.

## Metallic numerical code: MIT

`stdlib/runtime/math/{umuldi,rempio2}.gr` references and adapts
[Metallic](https://github.com/jdh8/metallic/blob/eb1aeb391602ce5fbae783261a44757a31242366/LICENSE).
The following upstream notice applies in addition to the Sun notices above:

MIT License

Copyright (C) 2017-2019 Chen-Pang He <chen.pang.he@jdh8.org>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

## musl scalbn code: MIT

`stdlib/runtime/numbers.gr` adapts musl `src/math/scalbn.c`, including the
[subnormal scaling correction](https://git.musl-libc.org/cgit/musl/commit/src/math/scalbn.c?id=8c44a060243f04283ca68dad199aab90336141db).
The [upstream notice at that revision](https://git.musl-libc.org/cgit/musl/tree/COPYRIGHT?id=8c44a060243f04283ca68dad199aab90336141db)
is reproduced below; the Sun-derived routines retain their separate notices above.

Copyright © 2005-2014 Rich Felker, et al.

Permission is hereby granted, free of charge, to any person obtaining
a copy of this software and associated documentation files (the
"Software"), to deal in the Software without restriction, including
without limitation the rights to use, copy, modify, merge, publish,
distribute, sublicense, and/or sell copies of the Software, and to
permit persons to whom the Software is furnished to do so, subject to
the following conditions:

The above copyright notice and this permission notice shall be
included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY
CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT,
TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

## UInt128 multiplication: BSD 2-Clause

`stdlib/runtime/atof/common.gr` adapts Jacob F. W.'s
[UInt128 multiplication article](https://www.codeproject.com/Tips/618570/UInt-Multiplication-Squaring).
The original article is no longer available. The author's BSD grant and the
matching multiplication sequence are retained by
[FreeRADIUS](https://github.com/FreeRADIUS/freeradius-server/blob/aeb8d49ba823ee69f8a032b25d1bbf3a1b0c5018/src/lib/util/uint128.h),
and the article's BSD grant is independently quoted by
[Zint](https://github.com/zint/zint/blob/4aec8df5cb3f5074925404a923a9c4d6bc44574e/backend/large.c#L33-L41).
This notice applies to the original multiplication code, not other FreeRADIUS
or Zint code.

Copyright 2014 Jacob F. W.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice,
   this list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

## Maciej Hirsz JSON number tests: MIT

The retained JSON test source attributes number tests to
https://github.com/maciejhirsz/json-rust/blob/master/tests/number.rs.
The separate JSON_checker-derived Validation module is excluded.

Copyright (c) 2016 Maciej Hirsz <maciej.hirsz@gmail.com>

The MIT License (MIT)

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
