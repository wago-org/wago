
experiments/loop-unroll-vectorization/results/native/dependent-i32-scalar.bin:     file format binary


Disassembly of section .data:

0000000000000000 <.data>:
   0:	48 89 f3             	mov    %rsi,%rbx
   3:	4c 8b bb e0 fe ff ff 	mov    -0x120(%rbx),%r15
   a:	51                   	push   %rcx
   b:	48 8b 07             	mov    (%rdi),%rax
   e:	48 8b 4f 08          	mov    0x8(%rdi),%rcx
  12:	48 8b 57 10          	mov    0x10(%rdi),%rdx
  16:	4c 8b 47 18          	mov    0x18(%rdi),%r8
  1a:	e8 11 00 00 00       	call   0x30
  1f:	5f                   	pop    %rdi
  20:	48 89 07             	mov    %rax,(%rdi)
  23:	48 89 57 08          	mov    %rdx,0x8(%rdi)
  27:	48 89 4f 10          	mov    %rcx,0x10(%rdi)
  2b:	4c 89 47 18          	mov    %r8,0x18(%rdi)
  2f:	c3                   	ret
  30:	48 81 ec 38 00 00 00 	sub    $0x38,%rsp
  37:	41 89 c4             	mov    %eax,%r12d
  3a:	41 89 cd             	mov    %ecx,%r13d
  3d:	41 89 d6             	mov    %edx,%r14d
  40:	45 89 c1             	mov    %r8d,%r9d
  43:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  4a:	00 00 
  4c:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  53:	00 00 
  55:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  5c:	00 00 
  5e:	66 90                	xchg   %ax,%ax
  60:	45 85 f6             	test   %r14d,%r14d
  63:	0f 84 40 00 00 00    	je     0xa9
  69:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  6e:	45 89 ed             	mov    %r13d,%r13d
  71:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  75:	4c 39 ff             	cmp    %r15,%rdi
  78:	0f 87 5b 00 00 00    	ja     0xd9
  7e:	46 03 0c 2b          	add    (%rbx,%r13,1),%r9d
  82:	45 89 e4             	mov    %r12d,%r12d
  85:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  8a:	4c 39 ff             	cmp    %r15,%rdi
  8d:	0f 87 46 00 00 00    	ja     0xd9
  93:	46 89 0c 23          	mov    %r9d,(%rbx,%r12,1)
  97:	41 83 c4 04          	add    $0x4,%r12d
  9b:	41 83 c5 04          	add    $0x4,%r13d
  9f:	41 83 ee 01          	sub    $0x1,%r14d
  a3:	0f 85 c5 ff ff ff    	jne    0x6e
  a9:	4c 89 64 24 10       	mov    %r12,0x10(%rsp)
  ae:	4c 89 6c 24 18       	mov    %r13,0x18(%rsp)
  b3:	4c 89 74 24 20       	mov    %r14,0x20(%rsp)
  b8:	4c 89 4c 24 28       	mov    %r9,0x28(%rsp)
  bd:	48 8b 44 24 10       	mov    0x10(%rsp),%rax
  c2:	48 8b 54 24 18       	mov    0x18(%rsp),%rdx
  c7:	48 8b 4c 24 20       	mov    0x20(%rsp),%rcx
  cc:	4c 8b 44 24 28       	mov    0x28(%rsp),%r8
  d1:	48 81 c4 38 00 00 00 	add    $0x38,%rsp
  d8:	c3                   	ret
  d9:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
  de:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
  e2:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
  e9:	89 46 14             	mov    %eax,0x14(%rsi)
  ec:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
  f2:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
  f6:	c3                   	ret
