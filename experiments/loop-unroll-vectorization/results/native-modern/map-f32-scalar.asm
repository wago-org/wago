
experiments/loop-unroll-vectorization/results/native-modern/map-f32-scalar.bin:     file format binary


Disassembly of section .data:

0000000000000000 <.data>:
   0:	48 81 ec 68 00 00 00 	sub    $0x68,%rsp
   7:	48 89 f3             	mov    %rsi,%rbx
   a:	48 89 4c 24 08       	mov    %rcx,0x8(%rsp)
   f:	4c 8b bb e0 fe ff ff 	mov    -0x120(%rbx),%r15
  16:	44 8b 27             	mov    (%rdi),%r12d
  19:	44 8b 6f 08          	mov    0x8(%rdi),%r13d
  1d:	44 8b 77 10          	mov    0x10(%rdi),%r14d
  21:	f3 44 0f 10 6f 18    	movss  0x18(%rdi),%xmm13
  27:	f3 44 0f 10 77 20    	movss  0x20(%rdi),%xmm14
  2d:	31 c0                	xor    %eax,%eax
  2f:	66 45 0f 57 e4       	xorpd  %xmm12,%xmm12
  34:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  3b:	00 00 
  3d:	0f 1f 00             	nopl   (%rax)
  40:	45 85 f6             	test   %r14d,%r14d
  43:	0f 84 49 00 00 00    	je     0x92
  49:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  4e:	45 89 ed             	mov    %r13d,%r13d
  51:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  55:	4c 39 ff             	cmp    %r15,%rdi
  58:	0f 87 7a 00 00 00    	ja     0xd8
  5e:	c4 a1 12 59 04 2b    	vmulss (%rbx,%r13,1),%xmm13,%xmm0
  64:	c4 41 7a 58 e6       	vaddss %xmm14,%xmm0,%xmm12
  69:	45 89 e4             	mov    %r12d,%r12d
  6c:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  71:	4c 39 ff             	cmp    %r15,%rdi
  74:	0f 87 5e 00 00 00    	ja     0xd8
  7a:	f3 46 0f 11 24 23    	movss  %xmm12,(%rbx,%r12,1)
  80:	41 83 c4 04          	add    $0x4,%r12d
  84:	41 83 c5 04          	add    $0x4,%r13d
  88:	41 83 ee 01          	sub    $0x1,%r14d
  8c:	0f 85 bc ff ff ff    	jne    0x4e
  92:	4c 89 64 24 38       	mov    %r12,0x38(%rsp)
  97:	4c 89 6c 24 40       	mov    %r13,0x40(%rsp)
  9c:	4c 89 74 24 48       	mov    %r14,0x48(%rsp)
  a1:	f2 44 0f 11 64 24 50 	movsd  %xmm12,0x50(%rsp)
  a8:	48 8b 7c 24 08       	mov    0x8(%rsp),%rdi
  ad:	48 8b 44 24 38       	mov    0x38(%rsp),%rax
  b2:	48 89 07             	mov    %rax,(%rdi)
  b5:	48 8b 44 24 40       	mov    0x40(%rsp),%rax
  ba:	48 89 47 08          	mov    %rax,0x8(%rdi)
  be:	48 8b 44 24 48       	mov    0x48(%rsp),%rax
  c3:	48 89 47 10          	mov    %rax,0x10(%rdi)
  c7:	48 8b 44 24 50       	mov    0x50(%rsp),%rax
  cc:	48 89 47 18          	mov    %rax,0x18(%rdi)
  d0:	48 81 c4 68 00 00 00 	add    $0x68,%rsp
  d7:	c3                   	ret
  d8:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
  dd:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
  e1:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
  e8:	89 46 14             	mov    %eax,0x14(%rsi)
  eb:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
  f1:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
  f5:	c3                   	ret
