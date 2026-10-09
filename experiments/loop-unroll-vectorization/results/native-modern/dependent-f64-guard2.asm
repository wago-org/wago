
experiments/loop-unroll-vectorization/results/native-modern/dependent-f64-guard2.bin:     file format binary


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
  30:	45 85 f6             	test   %r14d,%r14d
  33:	0f 84 85 00 00 00    	je     0xbe
  39:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  3e:	45 89 ed             	mov    %r13d,%r13d
  41:	49 8d 7d 08          	lea    0x8(%r13),%rdi
  45:	4c 39 ff             	cmp    %r15,%rdi
  48:	0f 87 b6 00 00 00    	ja     0x104
  4e:	c4 21 1b 58 24 2b    	vaddsd (%rbx,%r13,1),%xmm12,%xmm12
  54:	45 89 e4             	mov    %r12d,%r12d
  57:	49 8d 7c 24 08       	lea    0x8(%r12),%rdi
  5c:	4c 39 ff             	cmp    %r15,%rdi
  5f:	0f 87 9f 00 00 00    	ja     0x104
  65:	f2 46 0f 11 24 23    	movsd  %xmm12,(%rbx,%r12,1)
  6b:	41 83 c4 08          	add    $0x8,%r12d
  6f:	41 83 c5 08          	add    $0x8,%r13d
  73:	41 83 ee 01          	sub    $0x1,%r14d
  77:	45 85 f6             	test   %r14d,%r14d
  7a:	0f 84 3e 00 00 00    	je     0xbe
  80:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  85:	49 8d 7d 08          	lea    0x8(%r13),%rdi
  89:	4c 39 ff             	cmp    %r15,%rdi
  8c:	0f 87 72 00 00 00    	ja     0x104
  92:	c4 21 1b 58 24 2b    	vaddsd (%rbx,%r13,1),%xmm12,%xmm12
  98:	49 8d 7c 24 08       	lea    0x8(%r12),%rdi
  9d:	4c 39 ff             	cmp    %r15,%rdi
  a0:	0f 87 5e 00 00 00    	ja     0x104
  a6:	f2 46 0f 11 24 23    	movsd  %xmm12,(%rbx,%r12,1)
  ac:	41 83 c4 08          	add    $0x8,%r12d
  b0:	41 83 c5 08          	add    $0x8,%r13d
  b4:	41 83 ee 01          	sub    $0x1,%r14d
  b8:	0f 85 80 ff ff ff    	jne    0x3e
  be:	4c 89 64 24 28       	mov    %r12,0x28(%rsp)
  c3:	4c 89 6c 24 30       	mov    %r13,0x30(%rsp)
  c8:	4c 89 74 24 38       	mov    %r14,0x38(%rsp)
  cd:	f2 44 0f 11 64 24 40 	movsd  %xmm12,0x40(%rsp)
  d4:	48 8b 7c 24 08       	mov    0x8(%rsp),%rdi
  d9:	48 8b 44 24 28       	mov    0x28(%rsp),%rax
  de:	48 89 07             	mov    %rax,(%rdi)
  e1:	48 8b 44 24 30       	mov    0x30(%rsp),%rax
  e6:	48 89 47 08          	mov    %rax,0x8(%rdi)
  ea:	48 8b 44 24 38       	mov    0x38(%rsp),%rax
  ef:	48 89 47 10          	mov    %rax,0x10(%rdi)
  f3:	48 8b 44 24 40       	mov    0x40(%rsp),%rax
  f8:	48 89 47 18          	mov    %rax,0x18(%rdi)
  fc:	48 81 c4 58 00 00 00 	add    $0x58,%rsp
 103:	c3                   	ret
 104:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 109:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 10d:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 114:	89 46 14             	mov    %eax,0x14(%rsi)
 117:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 11d:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 121:	c3                   	ret
