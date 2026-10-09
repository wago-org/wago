
experiments/loop-unroll-vectorization/results/native/simd-i32-scalar.bin:     file format binary


Disassembly of section .data:

0000000000000000 <.data>:
   0:	48 81 ec 78 00 00 00 	sub    $0x78,%rsp
   7:	48 89 f3             	mov    %rsi,%rbx
   a:	48 89 4c 24 08       	mov    %rcx,0x8(%rsp)
   f:	4c 8b bb e0 fe ff ff 	mov    -0x120(%rbx),%r15
  16:	f3 44 0f 6f 6f 18    	movdqu 0x18(%rdi),%xmm13
  1c:	44 8b 27             	mov    (%rdi),%r12d
  1f:	44 8b 6f 08          	mov    0x8(%rdi),%r13d
  23:	44 8b 77 10          	mov    0x10(%rdi),%r14d
  27:	31 c0                	xor    %eax,%eax
  29:	66 45 0f 57 e4       	xorpd  %xmm12,%xmm12
  2e:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  35:	00 00 
  37:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  3e:	00 00 
  40:	45 85 f6             	test   %r14d,%r14d
  43:	0f 84 53 00 00 00    	je     0x9c
  49:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  4e:	45 89 ed             	mov    %r13d,%r13d
  51:	49 8d 7d 10          	lea    0x10(%r13),%rdi
  55:	4c 39 ff             	cmp    %r15,%rdi
  58:	0f 87 8a 00 00 00    	ja     0xe8
  5e:	f3 42 0f 6f 04 2b    	movdqu (%rbx,%r13,1),%xmm0
  64:	66 41 0f fe c5       	paddd  %xmm13,%xmm0
  69:	f3 44 0f 6f e0       	movdqu %xmm0,%xmm12
  6e:	f3 41 0f 6f c4       	movdqu %xmm12,%xmm0
  73:	45 89 e4             	mov    %r12d,%r12d
  76:	49 8d 7c 24 10       	lea    0x10(%r12),%rdi
  7b:	4c 39 ff             	cmp    %r15,%rdi
  7e:	0f 87 64 00 00 00    	ja     0xe8
  84:	f3 42 0f 7f 04 23    	movdqu %xmm0,(%rbx,%r12,1)
  8a:	41 83 c4 10          	add    $0x10,%r12d
  8e:	41 83 c5 10          	add    $0x10,%r13d
  92:	41 83 ee 01          	sub    $0x1,%r14d
  96:	0f 85 b2 ff ff ff    	jne    0x4e
  9c:	4c 89 64 24 40       	mov    %r12,0x40(%rsp)
  a1:	4c 89 6c 24 48       	mov    %r13,0x48(%rsp)
  a6:	4c 89 74 24 50       	mov    %r14,0x50(%rsp)
  ab:	f3 41 0f 6f c4       	movdqu %xmm12,%xmm0
  b0:	f3 0f 7f 44 24 58    	movdqu %xmm0,0x58(%rsp)
  b6:	48 8b 7c 24 08       	mov    0x8(%rsp),%rdi
  bb:	48 8b 44 24 40       	mov    0x40(%rsp),%rax
  c0:	48 89 07             	mov    %rax,(%rdi)
  c3:	48 8b 44 24 48       	mov    0x48(%rsp),%rax
  c8:	48 89 47 08          	mov    %rax,0x8(%rdi)
  cc:	48 8b 44 24 50       	mov    0x50(%rsp),%rax
  d1:	48 89 47 10          	mov    %rax,0x10(%rdi)
  d5:	f3 0f 6f 44 24 58    	movdqu 0x58(%rsp),%xmm0
  db:	f3 0f 7f 47 18       	movdqu %xmm0,0x18(%rdi)
  e0:	48 81 c4 78 00 00 00 	add    $0x78,%rsp
  e7:	c3                   	ret
  e8:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
  ed:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
  f1:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
  f8:	89 46 14             	mov    %eax,0x14(%rsi)
  fb:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 101:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 105:	c3                   	ret
