-----BEGIN PGP SIGNED MESSAGE-----
Hash: SHA512

Format: 3.0 (quilt)
Source: qemu
Binary: qemu-system, qemu-block-extra, qemu-system-data, qemu-system-common, qemu-system-gui, qemu-system-modules-spice, qemu-system-modules-opengl, qemu-system-misc, qemu-system-arm, qemu-system-mips, qemu-system-ppc, qemu-system-riscv, qemu-system-s390x, qemu-system-sparc, qemu-system-x86, qemu-system-xen, qemu-user, qemu-user-static, qemu-user-binfmt, qemu-utils, qemu-guest-agent
Architecture: linux-any all
Version: 1:10.0.13+ds-0+deb13u1
Maintainer: Debian QEMU Team <pkg-qemu-devel@lists.alioth.debian.org>
Uploaders: Michael Tokarev <mjt@tls.msk.ru>
Homepage: http://www.qemu.org/
Standards-Version: 4.7.2
Vcs-Browser: https://salsa.debian.org/qemu-team/qemu
Vcs-Git: https://salsa.debian.org/qemu-team/qemu.git
Testsuite: autopkgtest
Build-Depends: debhelper-compat (= 13), python3:any (>> 3.11), python3-venv:any, meson (>> 1.5.0~), ninja-build, flex, bison, python3-sphinx:native, python3-sphinx-rtd-theme:native
Build-Depends-Arch: pkgconf, libglib2.0-dev, zlib1g-dev, pkgconf:native, libglib2.0-dev:native, libcapstone-dev, libaio-dev [linux-any], libjack-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libpulse-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libasound2-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libpipewire-0.3-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libbpf-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libblkio-dev [amd64 arm64 loong64 mips64 mips64el ppc64 ppc64el riscv64 s390x sparc64], libbrlapi-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libcap-ng-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libcurl4-gnutls-dev, libfdt-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libfuse3-dev [linux-any], gnutls-dev, libgtk-3-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libvte-2.91-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libiscsi-dev, libncurses-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libvirglrenderer-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libepoxy-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libdrm-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libgbm-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libnfs-dev, libnuma-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libcacard-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libpixman-1-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], librbd-dev [amd64 arm64 loong64 mips64el ppc64el riscv64 s390x], libglusterfs-dev [amd64 arm64 loong64 mips64 mips64el ppc64 ppc64el riscv64 s390x sparc64], libsasl2-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libsdl2-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libseccomp-dev [amd64 arm64 armel armhf i386 loong64 mips64el mipsel ppc64 ppc64el powerpc riscv64 s390x], libslirp-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libspice-server-dev [amd64 i386 arm64 armel armhf loong64 mips64el mipsel ppc64el riscv64], librdmacm-dev [linux-any], libibverbs-dev [linux-any], libibumad-dev [linux-any], liburing-dev [linux-any], libusb-1.0-0-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libusbredirparser-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libssh-dev, libzstd-dev, libvdeplug-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libxen-dev [linux-amd64], nettle-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libudev-dev [linux-any], libjpeg-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libpng-dev [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64], libpmem-dev [amd64 arm64], seabios (>> 1.16.3-1~) [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64] <!nocheck>, qemu-system-data (>> 1:9.0) [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64] <!nocheck>, qemu-system-data (>> 1:9.2.0~rc3+ds-1~) [amd64 arm arm64 armel armhf i386 loong64 mips mips64 mips64el mipsel powerpc powerpcspe ppc64 ppc64el riscv64 s390x sparc sparc64] <!pkg.qemu.use-upstream-vdso !pkg.qemu.omit-vdso-build-dep>
Build-Depends-Indep: gcc-x86-64-linux-gnu, gcc-i686-linux-gnu, device-tree-compiler, gcc-s390x-linux-gnu, gcc-alpha-linux-gnu, gcc-powerpc-linux-gnu, gcc-powerpc64-linux-gnu, libssl-dev, gcc-sparc64-linux-gnu, fcode-utils, xsltproc, gcc-hppa-linux-gnu, gcc-hppa64-linux-gnu, gcc-riscv64-linux-gnu, gcc-arm-linux-gnueabi, libc6-dev-armel-cross, gcc-aarch64-linux-gnu, libc6-dev-arm64-cross, libc6-dev-hppa-cross <!pkg.qemu.use-upstream-vdso>, libc6-dev-i386-cross <!pkg.qemu.use-upstream-vdso>, gcc-loongarch64-linux-gnu <!pkg.qemu.use-upstream-vdso>, libc6-dev-loong64-cross <!pkg.qemu.use-upstream-vdso>, libc6-dev-ppc64-cross <!pkg.qemu.use-upstream-vdso>, libc6-dev-riscv64-cross <!pkg.qemu.use-upstream-vdso>, libc6-dev-s390x-cross <!pkg.qemu.use-upstream-vdso>
Build-Conflicts: oss4-dev
Package-List:
 qemu-block-extra deb otherosfs optional arch=alpha,amd64,arm,arm64,armel,armhf,hppa,i386,loong64,m68k,mips,mips64,mips64el,mipsel,powerpc,powerpcspe,ppc64,ppc64el,riscv64,s390x,sh4,sparc,sparc64,x32
 qemu-guest-agent deb otherosfs optional arch=linux-any
 qemu-system deb otherosfs optional arch=amd64,arm,arm64,armel,armhf,i386,loong64,mips,mips64,mips64el,mipsel,powerpc,powerpcspe,ppc64,ppc64el,riscv64,s390x,sparc,sparc64 profile=!pkg.qemu.omit-system
 qemu-system-arm deb otherosfs optional arch=amd64,arm,arm64,armel,armhf,i386,loong64,mips,mips64,mips64el,mipsel,powerpc,powerpcspe,ppc64,ppc64el,riscv64,s390x,sparc,sparc64 profile=!pkg.qemu.omit-system
 qemu-system-common deb otherosfs optional arch=amd64,arm,arm64,armel,armhf,i386,loong64,mips,mips64,mips64el,mipsel,powerpc,powerpcspe,ppc64,ppc64el,riscv64,s390x,sparc,sparc64 profile=!pkg.qemu.omit-system
 qemu-system-data deb otherosfs optional arch=all
 qemu-system-gui deb otherosfs optional arch=amd64,arm,arm64,armel,armhf,i386,loong64,mips,mips64,mips64el,mipsel,powerpc,powerpcspe,ppc64,ppc64el,riscv64,s390x,sparc,sparc64 profile=!pkg.qemu.omit-system
 qemu-system-mips deb otherosfs optional arch=amd64,arm,arm64,armel,armhf,i386,loong64,mips,mips64,mips64el,mipsel,powerpc,powerpcspe,ppc64,ppc64el,riscv64,s390x,sparc,sparc64 profile=!pkg.qemu.omit-system
 qemu-system-misc deb otherosfs optional arch=amd64,arm,arm64,armel,armhf,i386,loong64,mips,mips64,mips64el,mipsel,powerpc,powerpcspe,ppc64,ppc64el,riscv64,s390x,sparc,sparc64 profile=!pkg.qemu.omit-system
 qemu-system-modules-opengl deb otherosfs optional arch=amd64,arm,arm64,armel,armhf,i386,loong64,mips,mips64,mips64el,mipsel,powerpc,powerpcspe,ppc64,ppc64el,riscv64,s390x,sparc,sparc64 profile=!pkg.qemu.omit-system
 qemu-system-modules-spice deb otherosfs optional arch=amd64,i386,arm64,armel,armhf,loong64,mips64el,mipsel,ppc64el,riscv64 profile=!pkg.qemu.omit-system
 qemu-system-ppc deb otherosfs optional arch=amd64,arm,arm64,armel,armhf,i386,loong64,mips,mips64,mips64el,mipsel,powerpc,powerpcspe,ppc64,ppc64el,riscv64,s390x,sparc,sparc64 profile=!pkg.qemu.omit-system
 qemu-system-riscv deb otherosfs optional arch=amd64,arm,arm64,armel,armhf,i386,loong64,mips,mips64,mips64el,mipsel,powerpc,powerpcspe,ppc64,ppc64el,riscv64,s390x,sparc,sparc64 profile=!pkg.qemu.omit-system
 qemu-system-s390x deb otherosfs optional arch=amd64,arm,arm64,armel,armhf,i386,loong64,mips,mips64,mips64el,mipsel,powerpc,powerpcspe,ppc64,ppc64el,riscv64,s390x,sparc,sparc64 profile=!pkg.qemu.omit-system
 qemu-system-sparc deb otherosfs optional arch=amd64,arm,arm64,armel,armhf,i386,loong64,mips,mips64,mips64el,mipsel,powerpc,powerpcspe,ppc64,ppc64el,riscv64,s390x,sparc,sparc64 profile=!pkg.qemu.omit-system
 qemu-system-x86 deb otherosfs optional arch=amd64,arm,arm64,armel,armhf,i386,loong64,mips,mips64,mips64el,mipsel,powerpc,powerpcspe,ppc64,ppc64el,riscv64,s390x,sparc,sparc64 profile=!pkg.qemu.omit-system
 qemu-system-xen deb otherosfs optional arch=amd64 profile=!pkg.qemu.omit-system-xen
 qemu-user deb otherosfs optional arch=amd64,arm,arm64,armel,armhf,i386,loong64,mips,mips64,mips64el,mipsel,powerpc,powerpcspe,ppc64,ppc64el,riscv64,s390x,sparc,sparc64 profile=!pkg.qemu.omit-user
 qemu-user-binfmt deb otherosfs optional arch=amd64,arm,arm64,armel,armhf,i386,loong64,mips,mips64,mips64el,mipsel,powerpc,powerpcspe,ppc64,ppc64el,riscv64,s390x,sparc,sparc64 profile=!pkg.qemu.omit-user
 qemu-user-static deb oldlibs optional arch=amd64,arm,arm64,armel,armhf,i386,loong64,mips,mips64,mips64el,mipsel,powerpc,powerpcspe,ppc64,ppc64el,riscv64,s390x,sparc,sparc64 profile=!pkg.qemu.omit-user
 qemu-utils deb otherosfs optional arch=alpha,amd64,arm,arm64,armel,armhf,hppa,i386,loong64,m68k,mips,mips64,mips64el,mipsel,powerpc,powerpcspe,ppc64,ppc64el,riscv64,s390x,sh4,sparc,sparc64,x32
Checksums-Sha1:
 7617726069d7b00c2b7bd10aea8077b005913e5f 40010016 qemu_10.0.13+ds.orig.tar.xz
 8fd7672ce97e1b93405e6cb71e183158f87b6ff3 156096 qemu_10.0.13+ds-0+deb13u1.debian.tar.xz
Checksums-Sha256:
 6a0888e806c2ffc0c5f0733e974f9aea55491950d3c47c98669c8b236dde59ae 40010016 qemu_10.0.13+ds.orig.tar.xz
 da73c092f5684c551b0334d675eb16a3dec91e1edea94759cfb1102fa4a0364e 156096 qemu_10.0.13+ds-0+deb13u1.debian.tar.xz
Files:
 e9f804eae55071cd8ea1ae009f0fa1fd 40010016 qemu_10.0.13+ds.orig.tar.xz
 70708450eb8ee4642f13edfd4738c517 156096 qemu_10.0.13+ds-0+deb13u1.debian.tar.xz

-----BEGIN PGP SIGNATURE-----

iQIzBAEBCgAdFiEEZKoqtTHVaQM2a/75gqpKJDselHgFAmqbEZYACgkQgqpKJDse
lHiEZRAArgY4A73xMcPt6KY7MGEdFbf5/sWfQzPyvLOlg2NZ9dcREXIx++p3DXq6
HFBWJ23IbvOwAbkySjMcKJbxto6NRz7ZOt1z1IZfTV5bWELzWrPqZHM1+IU+B/qy
UJvmLHmigzbvX9c6WKEiu2HkKT0guyahQmSijFDpAgwhdqlu5VGHQu44Bgb00oDq
aikMDT//r+IMxtyVvyn7TTxCk8K1vsG+4eBaEtLCTW1Mmv8bRkSgKizxEb4R7QLu
+X4cT4oqT2bGmZsi+Ja4PQh9ZzVEEanV+Lvm09VZvvQ5JK7nVKMAmJ5oAkvZoIWF
GQmmII2NX33JBjfOGh0mZ5Y2YJ0vEiSFPxdH/aWn4whWn7hAik4P3/3XLziQzeHB
CqGA5Zr8n7F164DzwatUzQLVdi2jh0peis4a4htTsIhwKFFPA3kuCIcTCxYUv41L
f+GMr6eRVcxWOuxAyh7AZ44/aSsSMpJ6EdxTwcm3s4libfm3sPH3hONUq3FDkLHX
xo8gY92hlJ62xCG56r3JVrUn0Fi69tKNWuh+O26g0Yw21DPRGhBmYb55NP7V8XCT
eNWhQN+WGPuwCaHWs8wHOsdjzFT+Lkbhd/f4Mpkb4ZpX8PW7ir8dfivhIIrr94C/
WvbdLgLB986RTVBWYjTD6lXDFG9mC2cCxI1GHrSv/2UW3wiNzjg=
=G4eY
-----END PGP SIGNATURE-----
