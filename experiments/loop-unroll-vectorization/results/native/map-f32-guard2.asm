
experiments/loop-unroll-vectorization/results/native/map-f32-guard2.bin:     file format binary


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
  43:	0f 84 a3 00 00 00    	je     0xec
  49:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  4e:	45 89 ed             	mov    %r13d,%r13d
  51:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  55:	4c 39 ff             	cmp    %r15,%rdi
  58:	0f 87 d4 00 00 00    	ja     0x132
  5e:	f3 41 0f 10 c5       	movss  %xmm13,%xmm0
  63:	f3 42 0f 59 04 2b    	mulss  (%rbx,%r13,1),%xmm0
  69:	f3 44 0f 10 e0       	movss  %xmm0,%xmm12
  6e:	f3 45 0f 58 e6       	addss  %xmm14,%xmm12
  73:	45 89 e4             	mov    %r12d,%r12d
  76:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  7b:	4c 39 ff             	cmp    %r15,%rdi
  7e:	0f 87 ae 00 00 00    	ja     0x132
  84:	f3 46 0f 11 24 23    	movss  %xmm12,(%rbx,%r12,1)
  8a:	41 83 c4 04          	add    $0x4,%r12d
  8e:	41 83 c5 04          	add    $0x4,%r13d
  92:	41 83 ee 01          	sub    $0x1,%r14d
  96:	45 85 f6             	test   %r14d,%r14d
  99:	0f 84 4d 00 00 00    	je     0xec
  9f:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  a4:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  a8:	4c 39 ff             	cmp    %r15,%rdi
  ab:	0f 87 81 00 00 00    	ja     0x132
  b1:	f3 41 0f 10 c5       	movss  %xmm13,%xmm0
  b6:	f3 42 0f 59 04 2b    	mulss  (%rbx,%r13,1),%xmm0
  bc:	f3 44 0f 10 e0       	movss  %xmm0,%xmm12
  c1:	f3 45 0f 58 e6       	addss  %xmm14,%xmm12
  c6:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  cb:	4c 39 ff             	cmp    %r15,%rdi
  ce:	0f 87 5e 00 00 00    	ja     0x132
  d4:	f3 46 0f 11 24 23    	movss  %xmm12,(%rbx,%r12,1)
  da:	41 83 c4 04          	add    $0x4,%r12d
  de:	41 83 c5 04          	add    $0x4,%r13d
  e2:	41 83 ee 01          	sub    $0x1,%r14d
  e6:	0f 85 62 ff ff ff    	jne    0x4e
  ec:	4c 89 64 24 38       	mov    %r12,0x38(%rsp)
  f1:	4c 89 6c 24 40       	mov    %r13,0x40(%rsp)
  f6:	4c 89 74 24 48       	mov    %r14,0x48(%rsp)
  fb:	f2 44 0f 11 64 24 50 	movsd  %xmm12,0x50(%rsp)
 102:	48 8b 7c 24 08       	mov    0x8(%rsp),%rdi
 107:	48 8b 44 24 38       	mov    0x38(%rsp),%rax
 10c:	48 89 07             	mov    %rax,(%rdi)
 10f:	48 8b 44 24 40       	mov    0x40(%rsp),%rax
 114:	48 89 47 08          	mov    %rax,0x8(%rdi)
 118:	48 8b 44 24 48       	mov    0x48(%rsp),%rax
 11d:	48 89 47 10          	mov    %rax,0x10(%rdi)
 121:	48 8b 44 24 50       	mov    0x50(%rsp),%rax
 126:	48 89 47 18          	mov    %rax,0x18(%rdi)
 12a:	48 81 c4 68 00 00 00 	add    $0x68,%rsp
 131:	c3                   	ret
 132:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 137:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 13b:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 142:	89 46 14             	mov    %eax,0x14(%rsi)
 145:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 14b:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 14f:	c3                   	ret
