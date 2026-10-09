
experiments/loop-unroll-vectorization/results/native-modern/map-i32-scalar.bin:     file format binary


Disassembly of section .data:

0000000000000000 <.data>:
   0:	48 89 f3             	mov    %rsi,%rbx
   3:	4c 8b bb e0 fe ff ff 	mov    -0x120(%rbx),%r15
   a:	51                   	push   %rcx
   b:	48 8b 07             	mov    (%rdi),%rax
   e:	48 8b 4f 08          	mov    0x8(%rdi),%rcx
  12:	48 8b 57 10          	mov    0x10(%rdi),%rdx
  16:	4c 8b 47 18          	mov    0x18(%rdi),%r8
  1a:	4c 8b 4f 20          	mov    0x20(%rdi),%r9
  1e:	e8 11 00 00 00       	call   0x34
  23:	5f                   	pop    %rdi
  24:	48 89 07             	mov    %rax,(%rdi)
  27:	48 89 57 08          	mov    %rdx,0x8(%rdi)
  2b:	48 89 4f 10          	mov    %rcx,0x10(%rdi)
  2f:	4c 89 47 18          	mov    %r8,0x18(%rdi)
  33:	c3                   	ret
  34:	48 81 ec 48 00 00 00 	sub    $0x48,%rsp
  3b:	41 89 c4             	mov    %eax,%r12d
  3e:	41 89 cd             	mov    %ecx,%r13d
  41:	41 89 d6             	mov    %edx,%r14d
  44:	45 89 c3             	mov    %r8d,%r11d
  47:	44 89 cd             	mov    %r9d,%ebp
  4a:	31 c0                	xor    %eax,%eax
  4c:	45 31 d2             	xor    %r10d,%r10d
  4f:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  56:	00 00 
  58:	0f 1f 84 00 00 00 00 	nopl   0x0(%rax,%rax,1)
  5f:	00 
  60:	45 85 f6             	test   %r14d,%r14d
  63:	0f 84 47 00 00 00    	je     0xb0
  69:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  6e:	45 89 ed             	mov    %r13d,%r13d
  71:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  75:	4c 39 ff             	cmp    %r15,%rdi
  78:	0f 87 62 00 00 00    	ja     0xe0
  7e:	46 8b 14 2b          	mov    (%rbx,%r13,1),%r10d
  82:	45 0f af d3          	imul   %r11d,%r10d
  86:	41 01 ea             	add    %ebp,%r10d
  89:	45 89 e4             	mov    %r12d,%r12d
  8c:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  91:	4c 39 ff             	cmp    %r15,%rdi
  94:	0f 87 46 00 00 00    	ja     0xe0
  9a:	46 89 14 23          	mov    %r10d,(%rbx,%r12,1)
  9e:	41 83 c4 04          	add    $0x4,%r12d
  a2:	41 83 c5 04          	add    $0x4,%r13d
  a6:	41 83 ee 01          	sub    $0x1,%r14d
  aa:	0f 85 be ff ff ff    	jne    0x6e
  b0:	4c 89 64 24 18       	mov    %r12,0x18(%rsp)
  b5:	4c 89 6c 24 20       	mov    %r13,0x20(%rsp)
  ba:	4c 89 74 24 28       	mov    %r14,0x28(%rsp)
  bf:	4c 89 54 24 30       	mov    %r10,0x30(%rsp)
  c4:	48 8b 44 24 18       	mov    0x18(%rsp),%rax
  c9:	48 8b 54 24 20       	mov    0x20(%rsp),%rdx
  ce:	48 8b 4c 24 28       	mov    0x28(%rsp),%rcx
  d3:	4c 8b 44 24 30       	mov    0x30(%rsp),%r8
  d8:	48 81 c4 48 00 00 00 	add    $0x48,%rsp
  df:	c3                   	ret
  e0:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
  e5:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
  e9:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
  f0:	89 46 14             	mov    %eax,0x14(%rsi)
  f3:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
  f9:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
  fd:	c3                   	ret
