
experiments/specialized-sum-unroll/results/native-C/pressure4.bin:     file format binary


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
  1e:	4c 8b 57 28          	mov    0x28(%rdi),%r10
  22:	4c 8b 5f 30          	mov    0x30(%rdi),%r11
  26:	e8 11 00 00 00       	call   0x3c
  2b:	5f                   	pop    %rdi
  2c:	48 89 07             	mov    %rax,(%rdi)
  2f:	48 89 57 08          	mov    %rdx,0x8(%rdi)
  33:	48 89 4f 10          	mov    %rcx,0x10(%rdi)
  37:	4c 89 47 18          	mov    %r8,0x18(%rdi)
  3b:	c3                   	ret
  3c:	48 81 ec 58 00 00 00 	sub    $0x58,%rsp
  43:	41 89 c4             	mov    %eax,%r12d
  46:	41 89 cd             	mov    %ecx,%r13d
  49:	49 89 d6             	mov    %rdx,%r14
  4c:	4c 89 c5             	mov    %r8,%rbp
  4f:	4c 89 4c 24 18       	mov    %r9,0x18(%rsp)
  54:	4c 89 54 24 20       	mov    %r10,0x20(%rsp)
  59:	4c 89 5c 24 28       	mov    %r11,0x28(%rsp)
  5e:	66 90                	xchg   %ax,%ax
  60:	45 85 ed             	test   %r13d,%r13d
  63:	0f 84 ab 00 00 00    	je     0x114
  69:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  6e:	44 89 ef             	mov    %r13d,%edi
  71:	48 c1 e7 03          	shl    $0x3,%rdi
  75:	45 89 e4             	mov    %r12d,%r12d
  78:	4c 01 e7             	add    %r12,%rdi
  7b:	4c 39 ff             	cmp    %r15,%rdi
  7e:	0f 86 20 00 00 00    	jbe    0xa4
  84:	48 bf 00 00 00 00 01 	movabs $0x100000000,%rdi
  8b:	00 00 00 
  8e:	4c 39 ff             	cmp    %r15,%rdi
  91:	0f 85 c0 00 00 00    	jne    0x157
  97:	41 f7 c4 07 00 00 00 	test   $0x7,%r12d
  9e:	0f 85 b3 00 00 00    	jne    0x157
  a4:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
  a8:	41 83 c4 08          	add    $0x8,%r12d
  ac:	31 ff                	xor    %edi,%edi
  ae:	41 83 ed 01          	sub    $0x1,%r13d
  b2:	0f 84 59 00 00 00    	je     0x111
  b8:	41 83 fd 02          	cmp    $0x2,%r13d
  bc:	0f 82 34 00 00 00    	jb     0xf6
  c2:	44 89 ef             	mov    %r13d,%edi
  c5:	48 c1 e7 03          	shl    $0x3,%rdi
  c9:	4c 01 e7             	add    %r12,%rdi
  cc:	48 c1 ef 20          	shr    $0x20,%rdi
  d0:	bf 00 00 00 00       	mov    $0x0,%edi
  d5:	0f 85 1b 00 00 00    	jne    0xf6
  db:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
  df:	4a 03 7c 23 08       	add    0x8(%rbx,%r12,1),%rdi
  e4:	41 83 c4 10          	add    $0x10,%r12d
  e8:	41 83 ed 02          	sub    $0x2,%r13d
  ec:	41 83 fd 02          	cmp    $0x2,%r13d
  f0:	0f 83 e5 ff ff ff    	jae    0xdb
  f6:	45 85 ed             	test   %r13d,%r13d
  f9:	0f 84 12 00 00 00    	je     0x111
  ff:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
 103:	41 83 c4 08          	add    $0x8,%r12d
 107:	41 83 ed 01          	sub    $0x1,%r13d
 10b:	0f 85 ee ff ff ff    	jne    0xff
 111:	49 01 fe             	add    %rdi,%r14
 114:	4c 89 74 24 30       	mov    %r14,0x30(%rsp)
 119:	4c 89 64 24 38       	mov    %r12,0x38(%rsp)
 11e:	4c 89 6c 24 40       	mov    %r13,0x40(%rsp)
 123:	48 8d 7d 00          	lea    0x0(%rbp),%rdi
 127:	48 03 7c 24 18       	add    0x18(%rsp),%rdi
 12c:	48 03 7c 24 20       	add    0x20(%rsp),%rdi
 131:	48 03 7c 24 28       	add    0x28(%rsp),%rdi
 136:	48 89 7c 24 48       	mov    %rdi,0x48(%rsp)
 13b:	48 8b 44 24 30       	mov    0x30(%rsp),%rax
 140:	48 8b 54 24 38       	mov    0x38(%rsp),%rdx
 145:	48 8b 4c 24 40       	mov    0x40(%rsp),%rcx
 14a:	4c 8b 44 24 48       	mov    0x48(%rsp),%r8
 14f:	48 81 c4 58 00 00 00 	add    $0x58,%rsp
 156:	c3                   	ret
 157:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 15c:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 160:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 167:	89 46 14             	mov    %eax,0x14(%rsi)
 16a:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 170:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 174:	c3                   	ret
