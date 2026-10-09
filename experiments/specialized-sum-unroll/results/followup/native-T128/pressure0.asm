
experiments/specialized-sum-unroll/results/followup/native-T128/pressure0.bin:     file format binary


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
  43:	0f 84 35 01 00 00    	je     0x17e
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
  71:	0f 85 3c 01 00 00    	jne    0x1b3
  77:	41 f7 c4 07 00 00 00 	test   $0x7,%r12d
  7e:	0f 85 2f 01 00 00    	jne    0x1b3
  84:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
  88:	41 83 c4 08          	add    $0x8,%r12d
  8c:	31 ff                	xor    %edi,%edi
  8e:	31 f6                	xor    %esi,%esi
  90:	31 ed                	xor    %ebp,%ebp
  92:	41 83 ed 01          	sub    $0x1,%r13d
  96:	0f 84 d9 00 00 00    	je     0x175
  9c:	41 83 fd 04          	cmp    $0x4,%r13d
  a0:	0f 82 b4 00 00 00    	jb     0x15a
  a6:	44 89 ef             	mov    %r13d,%edi
  a9:	48 c1 e7 03          	shl    $0x3,%rdi
  ad:	4c 01 e7             	add    %r12,%rdi
  b0:	48 c1 ef 20          	shr    $0x20,%rdi
  b4:	bf 00 00 00 00       	mov    $0x0,%edi
  b9:	0f 85 9b 00 00 00    	jne    0x15a
  bf:	41 81 fd 80 00 00 00 	cmp    $0x80,%r13d
  c6:	0f 82 69 00 00 00    	jb     0x135
  cc:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
  d0:	4a 03 7c 23 08       	add    0x8(%rbx,%r12,1),%rdi
  d5:	4a 03 74 23 10       	add    0x10(%rbx,%r12,1),%rsi
  da:	4a 03 6c 23 18       	add    0x18(%rbx,%r12,1),%rbp
  df:	4e 03 74 23 20       	add    0x20(%rbx,%r12,1),%r14
  e4:	4a 03 7c 23 28       	add    0x28(%rbx,%r12,1),%rdi
  e9:	4a 03 74 23 30       	add    0x30(%rbx,%r12,1),%rsi
  ee:	4a 03 6c 23 38       	add    0x38(%rbx,%r12,1),%rbp
  f3:	4e 03 74 23 40       	add    0x40(%rbx,%r12,1),%r14
  f8:	4a 03 7c 23 48       	add    0x48(%rbx,%r12,1),%rdi
  fd:	4a 03 74 23 50       	add    0x50(%rbx,%r12,1),%rsi
 102:	4a 03 6c 23 58       	add    0x58(%rbx,%r12,1),%rbp
 107:	4e 03 74 23 60       	add    0x60(%rbx,%r12,1),%r14
 10c:	4a 03 7c 23 68       	add    0x68(%rbx,%r12,1),%rdi
 111:	4a 03 74 23 70       	add    0x70(%rbx,%r12,1),%rsi
 116:	4a 03 6c 23 78       	add    0x78(%rbx,%r12,1),%rbp
 11b:	41 81 c4 80 00 00 00 	add    $0x80,%r12d
 122:	41 83 ed 10          	sub    $0x10,%r13d
 126:	41 83 fd 10          	cmp    $0x10,%r13d
 12a:	0f 83 9c ff ff ff    	jae    0xcc
 130:	e9 25 00 00 00       	jmp    0x15a
 135:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
 139:	4a 03 7c 23 08       	add    0x8(%rbx,%r12,1),%rdi
 13e:	4a 03 74 23 10       	add    0x10(%rbx,%r12,1),%rsi
 143:	4a 03 6c 23 18       	add    0x18(%rbx,%r12,1),%rbp
 148:	41 83 c4 20          	add    $0x20,%r12d
 14c:	41 83 ed 04          	sub    $0x4,%r13d
 150:	41 83 fd 04          	cmp    $0x4,%r13d
 154:	0f 83 db ff ff ff    	jae    0x135
 15a:	45 85 ed             	test   %r13d,%r13d
 15d:	0f 84 12 00 00 00    	je     0x175
 163:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
 167:	41 83 c4 08          	add    $0x8,%r12d
 16b:	41 83 ed 01          	sub    $0x1,%r13d
 16f:	0f 85 ee ff ff ff    	jne    0x163
 175:	49 01 fe             	add    %rdi,%r14
 178:	49 01 f6             	add    %rsi,%r14
 17b:	49 01 ee             	add    %rbp,%r14
 17e:	4c 89 74 24 10       	mov    %r14,0x10(%rsp)
 183:	4c 89 64 24 18       	mov    %r12,0x18(%rsp)
 188:	4c 89 6c 24 20       	mov    %r13,0x20(%rsp)
 18d:	bf 00 00 00 00       	mov    $0x0,%edi
 192:	48 89 7c 24 28       	mov    %rdi,0x28(%rsp)
 197:	48 8b 44 24 10       	mov    0x10(%rsp),%rax
 19c:	48 8b 54 24 18       	mov    0x18(%rsp),%rdx
 1a1:	48 8b 4c 24 20       	mov    0x20(%rsp),%rcx
 1a6:	4c 8b 44 24 28       	mov    0x28(%rsp),%r8
 1ab:	48 81 c4 38 00 00 00 	add    $0x38,%rsp
 1b2:	c3                   	ret
 1b3:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 1b8:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 1bc:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 1c3:	89 46 14             	mov    %eax,0x14(%rsi)
 1c6:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 1cc:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 1d0:	c3                   	ret
