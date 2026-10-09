
experiments/loop-unroll-vectorization/results/native-modern/map-f32-guard2.bin:     file format binary


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
  43:	0f 84 8f 00 00 00    	je     0xd8
  49:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  4e:	45 89 ed             	mov    %r13d,%r13d
  51:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  55:	4c 39 ff             	cmp    %r15,%rdi
  58:	0f 87 c0 00 00 00    	ja     0x11e
  5e:	c4 a1 12 59 04 2b    	vmulss (%rbx,%r13,1),%xmm13,%xmm0
  64:	c4 41 7a 58 e6       	vaddss %xmm14,%xmm0,%xmm12
  69:	45 89 e4             	mov    %r12d,%r12d
  6c:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  71:	4c 39 ff             	cmp    %r15,%rdi
  74:	0f 87 a4 00 00 00    	ja     0x11e
  7a:	f3 46 0f 11 24 23    	movss  %xmm12,(%rbx,%r12,1)
  80:	41 83 c4 04          	add    $0x4,%r12d
  84:	41 83 c5 04          	add    $0x4,%r13d
  88:	41 83 ee 01          	sub    $0x1,%r14d
  8c:	45 85 f6             	test   %r14d,%r14d
  8f:	0f 84 43 00 00 00    	je     0xd8
  95:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  9a:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  9e:	4c 39 ff             	cmp    %r15,%rdi
  a1:	0f 87 77 00 00 00    	ja     0x11e
  a7:	c4 a1 12 59 04 2b    	vmulss (%rbx,%r13,1),%xmm13,%xmm0
  ad:	c4 41 7a 58 e6       	vaddss %xmm14,%xmm0,%xmm12
  b2:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  b7:	4c 39 ff             	cmp    %r15,%rdi
  ba:	0f 87 5e 00 00 00    	ja     0x11e
  c0:	f3 46 0f 11 24 23    	movss  %xmm12,(%rbx,%r12,1)
  c6:	41 83 c4 04          	add    $0x4,%r12d
  ca:	41 83 c5 04          	add    $0x4,%r13d
  ce:	41 83 ee 01          	sub    $0x1,%r14d
  d2:	0f 85 76 ff ff ff    	jne    0x4e
  d8:	4c 89 64 24 38       	mov    %r12,0x38(%rsp)
  dd:	4c 89 6c 24 40       	mov    %r13,0x40(%rsp)
  e2:	4c 89 74 24 48       	mov    %r14,0x48(%rsp)
  e7:	f2 44 0f 11 64 24 50 	movsd  %xmm12,0x50(%rsp)
  ee:	48 8b 7c 24 08       	mov    0x8(%rsp),%rdi
  f3:	48 8b 44 24 38       	mov    0x38(%rsp),%rax
  f8:	48 89 07             	mov    %rax,(%rdi)
  fb:	48 8b 44 24 40       	mov    0x40(%rsp),%rax
 100:	48 89 47 08          	mov    %rax,0x8(%rdi)
 104:	48 8b 44 24 48       	mov    0x48(%rsp),%rax
 109:	48 89 47 10          	mov    %rax,0x10(%rdi)
 10d:	48 8b 44 24 50       	mov    0x50(%rsp),%rax
 112:	48 89 47 18          	mov    %rax,0x18(%rdi)
 116:	48 81 c4 68 00 00 00 	add    $0x68,%rsp
 11d:	c3                   	ret
 11e:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 123:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 127:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 12e:	89 46 14             	mov    %eax,0x14(%rsi)
 131:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 137:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 13b:	c3                   	ret
