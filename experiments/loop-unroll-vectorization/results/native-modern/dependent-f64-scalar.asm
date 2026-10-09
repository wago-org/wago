
experiments/loop-unroll-vectorization/results/native-modern/dependent-f64-scalar.bin:     file format binary


Disassembly of section .data:

0000000000000000 <.data>:
   0:	48 81 ec 58 00 00 00 	sub    $0x58,%rsp
   7:	48 89 f3             	mov    %rsi,%rbx
   a:	48 89 4c 24 08       	mov    %rcx,0x8(%rsp)
   f:	4c 8b bb e0 fe ff ff 	mov    -0x120(%rbx),%r15
  16:	44 8b 27             	mov    (%rdi),%r12d
  19:	44 8b 6f 08          	mov    0x8(%rdi),%r13d
  1d:	44 8b 77 10          	mov    0x10(%rdi),%r14d
  21:	f2 44 0f 10 67 18    	movsd  0x18(%rdi),%xmm12
  27:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  2e:	00 00 
  30:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  37:	00 00 
  39:	0f 1f 80 00 00 00 00 	nopl   0x0(%rax)
  40:	45 85 f6             	test   %r14d,%r14d
  43:	0f 84 44 00 00 00    	je     0x8d
  49:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  4e:	45 89 ed             	mov    %r13d,%r13d
  51:	49 8d 7d 08          	lea    0x8(%r13),%rdi
  55:	4c 39 ff             	cmp    %r15,%rdi
  58:	0f 87 75 00 00 00    	ja     0xd3
  5e:	c4 21 1b 58 24 2b    	vaddsd (%rbx,%r13,1),%xmm12,%xmm12
  64:	45 89 e4             	mov    %r12d,%r12d
  67:	49 8d 7c 24 08       	lea    0x8(%r12),%rdi
  6c:	4c 39 ff             	cmp    %r15,%rdi
  6f:	0f 87 5e 00 00 00    	ja     0xd3
  75:	f2 46 0f 11 24 23    	movsd  %xmm12,(%rbx,%r12,1)
  7b:	41 83 c4 08          	add    $0x8,%r12d
  7f:	41 83 c5 08          	add    $0x8,%r13d
  83:	41 83 ee 01          	sub    $0x1,%r14d
  87:	0f 85 c1 ff ff ff    	jne    0x4e
  8d:	4c 89 64 24 28       	mov    %r12,0x28(%rsp)
  92:	4c 89 6c 24 30       	mov    %r13,0x30(%rsp)
  97:	4c 89 74 24 38       	mov    %r14,0x38(%rsp)
  9c:	f2 44 0f 11 64 24 40 	movsd  %xmm12,0x40(%rsp)
  a3:	48 8b 7c 24 08       	mov    0x8(%rsp),%rdi
  a8:	48 8b 44 24 28       	mov    0x28(%rsp),%rax
  ad:	48 89 07             	mov    %rax,(%rdi)
  b0:	48 8b 44 24 30       	mov    0x30(%rsp),%rax
  b5:	48 89 47 08          	mov    %rax,0x8(%rdi)
  b9:	48 8b 44 24 38       	mov    0x38(%rsp),%rax
  be:	48 89 47 10          	mov    %rax,0x10(%rdi)
  c2:	48 8b 44 24 40       	mov    0x40(%rsp),%rax
  c7:	48 89 47 18          	mov    %rax,0x18(%rdi)
  cb:	48 81 c4 58 00 00 00 	add    $0x58,%rsp
  d2:	c3                   	ret
  d3:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
  d8:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
  dc:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
  e3:	89 46 14             	mov    %eax,0x14(%rsi)
  e6:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
  ec:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
  f0:	c3                   	ret
