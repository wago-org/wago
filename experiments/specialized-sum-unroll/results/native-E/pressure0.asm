
experiments/specialized-sum-unroll/results/native-E/pressure0.bin:     file format binary


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
  43:	0f 84 15 01 00 00    	je     0x15e
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
  71:	0f 85 1c 01 00 00    	jne    0x193
  77:	41 f7 c4 07 00 00 00 	test   $0x7,%r12d
  7e:	0f 85 0f 01 00 00    	jne    0x193
  84:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
  88:	41 83 c4 08          	add    $0x8,%r12d
  8c:	31 ff                	xor    %edi,%edi
  8e:	31 f6                	xor    %esi,%esi
  90:	31 ed                	xor    %ebp,%ebp
  92:	45 31 c9             	xor    %r9d,%r9d
  95:	45 31 d2             	xor    %r10d,%r10d
  98:	45 31 db             	xor    %r11d,%r11d
  9b:	31 c0                	xor    %eax,%eax
  9d:	41 83 ed 01          	sub    $0x1,%r13d
  a1:	0f 84 a2 00 00 00    	je     0x149
  a7:	41 83 fd 10          	cmp    $0x10,%r13d
  ab:	0f 82 7d 00 00 00    	jb     0x12e
  b1:	44 89 ef             	mov    %r13d,%edi
  b4:	48 c1 e7 03          	shl    $0x3,%rdi
  b8:	4c 01 e7             	add    %r12,%rdi
  bb:	48 c1 ef 20          	shr    $0x20,%rdi
  bf:	bf 00 00 00 00       	mov    $0x0,%edi
  c4:	0f 85 64 00 00 00    	jne    0x12e
  ca:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
  ce:	4a 03 7c 23 08       	add    0x8(%rbx,%r12,1),%rdi
  d3:	4a 03 74 23 10       	add    0x10(%rbx,%r12,1),%rsi
  d8:	4a 03 6c 23 18       	add    0x18(%rbx,%r12,1),%rbp
  dd:	4e 03 4c 23 20       	add    0x20(%rbx,%r12,1),%r9
  e2:	4e 03 54 23 28       	add    0x28(%rbx,%r12,1),%r10
  e7:	4e 03 5c 23 30       	add    0x30(%rbx,%r12,1),%r11
  ec:	4a 03 44 23 38       	add    0x38(%rbx,%r12,1),%rax
  f1:	4e 03 74 23 40       	add    0x40(%rbx,%r12,1),%r14
  f6:	4a 03 7c 23 48       	add    0x48(%rbx,%r12,1),%rdi
  fb:	4a 03 74 23 50       	add    0x50(%rbx,%r12,1),%rsi
 100:	4a 03 6c 23 58       	add    0x58(%rbx,%r12,1),%rbp
 105:	4e 03 4c 23 60       	add    0x60(%rbx,%r12,1),%r9
 10a:	4e 03 54 23 68       	add    0x68(%rbx,%r12,1),%r10
 10f:	4e 03 5c 23 70       	add    0x70(%rbx,%r12,1),%r11
 114:	4a 03 44 23 78       	add    0x78(%rbx,%r12,1),%rax
 119:	41 81 c4 80 00 00 00 	add    $0x80,%r12d
 120:	41 83 ed 10          	sub    $0x10,%r13d
 124:	41 83 fd 10          	cmp    $0x10,%r13d
 128:	0f 83 9c ff ff ff    	jae    0xca
 12e:	45 85 ed             	test   %r13d,%r13d
 131:	0f 84 12 00 00 00    	je     0x149
 137:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
 13b:	41 83 c4 08          	add    $0x8,%r12d
 13f:	41 83 ed 01          	sub    $0x1,%r13d
 143:	0f 85 ee ff ff ff    	jne    0x137
 149:	49 01 fe             	add    %rdi,%r14
 14c:	49 01 f6             	add    %rsi,%r14
 14f:	49 01 ee             	add    %rbp,%r14
 152:	4d 01 ce             	add    %r9,%r14
 155:	4d 01 d6             	add    %r10,%r14
 158:	4d 01 de             	add    %r11,%r14
 15b:	49 01 c6             	add    %rax,%r14
 15e:	4c 89 74 24 10       	mov    %r14,0x10(%rsp)
 163:	4c 89 64 24 18       	mov    %r12,0x18(%rsp)
 168:	4c 89 6c 24 20       	mov    %r13,0x20(%rsp)
 16d:	bf 00 00 00 00       	mov    $0x0,%edi
 172:	48 89 7c 24 28       	mov    %rdi,0x28(%rsp)
 177:	48 8b 44 24 10       	mov    0x10(%rsp),%rax
 17c:	48 8b 54 24 18       	mov    0x18(%rsp),%rdx
 181:	48 8b 4c 24 20       	mov    0x20(%rsp),%rcx
 186:	4c 8b 44 24 28       	mov    0x28(%rsp),%r8
 18b:	48 81 c4 38 00 00 00 	add    $0x38,%rsp
 192:	c3                   	ret
 193:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 198:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 19c:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 1a3:	89 46 14             	mov    %eax,0x14(%rsi)
 1a6:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 1ac:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 1b0:	c3                   	ret
