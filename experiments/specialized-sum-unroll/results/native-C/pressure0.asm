
experiments/specialized-sum-unroll/results/native-C/pressure0.bin:     file format binary


Disassembly of section .data:

0000000000000000 <.data>:
   0:	48 89 f3             	mov    %rsi,%rbx
   3:	4c 8b bb e0 fe ff ff 	mov    -0x120(%rbx),%r15
   a:	51                   	push   %rcx
   b:	48 8b 07             	mov    (%rdi),%rax
   e:	48 8b 4f 08          	mov    0x8(%rdi),%rcx
  12:	48 8b 57 10          	mov    0x10(%rdi),%rdx
  16:	e8 11 00 00 00       	call   0x2c
  1b:	5f                   	pop    %rdi
  1c:	48 89 07             	mov    %rax,(%rdi)
  1f:	48 89 57 08          	mov    %rdx,0x8(%rdi)
  23:	48 89 4f 10          	mov    %rcx,0x10(%rdi)
  27:	4c 89 47 18          	mov    %r8,0x18(%rdi)
  2b:	c3                   	ret
  2c:	48 81 ec 38 00 00 00 	sub    $0x38,%rsp
  33:	41 89 c4             	mov    %eax,%r12d
  36:	41 89 cd             	mov    %ecx,%r13d
  39:	49 89 d6             	mov    %rdx,%r14
  3c:	0f 1f 40 00          	nopl   0x0(%rax)
  40:	45 85 ed             	test   %r13d,%r13d
  43:	0f 84 ab 00 00 00    	je     0xf4
  49:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  4e:	44 89 ef             	mov    %r13d,%edi
  51:	48 c1 e7 03          	shl    $0x3,%rdi
  55:	45 89 e4             	mov    %r12d,%r12d
  58:	4c 01 e7             	add    %r12,%rdi
  5b:	4c 39 ff             	cmp    %r15,%rdi
  5e:	0f 86 20 00 00 00    	jbe    0x84
  64:	48 bf 00 00 00 00 01 	movabs $0x100000000,%rdi
  6b:	00 00 00 
  6e:	4c 39 ff             	cmp    %r15,%rdi
  71:	0f 85 b2 00 00 00    	jne    0x129
  77:	41 f7 c4 07 00 00 00 	test   $0x7,%r12d
  7e:	0f 85 a5 00 00 00    	jne    0x129
  84:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
  88:	41 83 c4 08          	add    $0x8,%r12d
  8c:	31 ff                	xor    %edi,%edi
  8e:	41 83 ed 01          	sub    $0x1,%r13d
  92:	0f 84 59 00 00 00    	je     0xf1
  98:	41 83 fd 02          	cmp    $0x2,%r13d
  9c:	0f 82 34 00 00 00    	jb     0xd6
  a2:	44 89 ef             	mov    %r13d,%edi
  a5:	48 c1 e7 03          	shl    $0x3,%rdi
  a9:	4c 01 e7             	add    %r12,%rdi
  ac:	48 c1 ef 20          	shr    $0x20,%rdi
  b0:	bf 00 00 00 00       	mov    $0x0,%edi
  b5:	0f 85 1b 00 00 00    	jne    0xd6
  bb:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
  bf:	4a 03 7c 23 08       	add    0x8(%rbx,%r12,1),%rdi
  c4:	41 83 c4 10          	add    $0x10,%r12d
  c8:	41 83 ed 02          	sub    $0x2,%r13d
  cc:	41 83 fd 02          	cmp    $0x2,%r13d
  d0:	0f 83 e5 ff ff ff    	jae    0xbb
  d6:	45 85 ed             	test   %r13d,%r13d
  d9:	0f 84 12 00 00 00    	je     0xf1
  df:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
  e3:	41 83 c4 08          	add    $0x8,%r12d
  e7:	41 83 ed 01          	sub    $0x1,%r13d
  eb:	0f 85 ee ff ff ff    	jne    0xdf
  f1:	49 01 fe             	add    %rdi,%r14
  f4:	4c 89 74 24 10       	mov    %r14,0x10(%rsp)
  f9:	4c 89 64 24 18       	mov    %r12,0x18(%rsp)
  fe:	4c 89 6c 24 20       	mov    %r13,0x20(%rsp)
 103:	bf 00 00 00 00       	mov    $0x0,%edi
 108:	48 89 7c 24 28       	mov    %rdi,0x28(%rsp)
 10d:	48 8b 44 24 10       	mov    0x10(%rsp),%rax
 112:	48 8b 54 24 18       	mov    0x18(%rsp),%rdx
 117:	48 8b 4c 24 20       	mov    0x20(%rsp),%rcx
 11c:	4c 8b 44 24 28       	mov    0x28(%rsp),%r8
 121:	48 81 c4 38 00 00 00 	add    $0x38,%rsp
 128:	c3                   	ret
 129:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 12e:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 132:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 139:	89 46 14             	mov    %eax,0x14(%rsi)
 13c:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 142:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 146:	c3                   	ret
