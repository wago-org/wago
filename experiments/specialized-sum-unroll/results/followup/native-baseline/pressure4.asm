
experiments/specialized-sum-unroll/results/followup/native-baseline/pressure4.bin:     file format binary


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
  63:	0f 84 c0 00 00 00    	je     0x129
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
  91:	0f 85 d5 00 00 00    	jne    0x16c
  97:	41 f7 c4 07 00 00 00 	test   $0x7,%r12d
  9e:	0f 85 c8 00 00 00    	jne    0x16c
  a4:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
  a8:	41 83 c4 08          	add    $0x8,%r12d
  ac:	31 ff                	xor    %edi,%edi
  ae:	31 f6                	xor    %esi,%esi
  b0:	45 31 c9             	xor    %r9d,%r9d
  b3:	41 83 ed 01          	sub    $0x1,%r13d
  b7:	0f 84 63 00 00 00    	je     0x120
  bd:	41 83 fd 04          	cmp    $0x4,%r13d
  c1:	0f 82 3e 00 00 00    	jb     0x105
  c7:	44 89 ef             	mov    %r13d,%edi
  ca:	48 c1 e7 03          	shl    $0x3,%rdi
  ce:	4c 01 e7             	add    %r12,%rdi
  d1:	48 c1 ef 20          	shr    $0x20,%rdi
  d5:	bf 00 00 00 00       	mov    $0x0,%edi
  da:	0f 85 25 00 00 00    	jne    0x105
  e0:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
  e4:	4a 03 7c 23 08       	add    0x8(%rbx,%r12,1),%rdi
  e9:	4a 03 74 23 10       	add    0x10(%rbx,%r12,1),%rsi
  ee:	4e 03 4c 23 18       	add    0x18(%rbx,%r12,1),%r9
  f3:	41 83 c4 20          	add    $0x20,%r12d
  f7:	41 83 ed 04          	sub    $0x4,%r13d
  fb:	41 83 fd 04          	cmp    $0x4,%r13d
  ff:	0f 83 db ff ff ff    	jae    0xe0
 105:	45 85 ed             	test   %r13d,%r13d
 108:	0f 84 12 00 00 00    	je     0x120
 10e:	4e 03 34 23          	add    (%rbx,%r12,1),%r14
 112:	41 83 c4 08          	add    $0x8,%r12d
 116:	41 83 ed 01          	sub    $0x1,%r13d
 11a:	0f 85 ee ff ff ff    	jne    0x10e
 120:	49 01 fe             	add    %rdi,%r14
 123:	49 01 f6             	add    %rsi,%r14
 126:	4d 01 ce             	add    %r9,%r14
 129:	4c 89 74 24 30       	mov    %r14,0x30(%rsp)
 12e:	4c 89 64 24 38       	mov    %r12,0x38(%rsp)
 133:	4c 89 6c 24 40       	mov    %r13,0x40(%rsp)
 138:	48 8d 7d 00          	lea    0x0(%rbp),%rdi
 13c:	48 03 7c 24 18       	add    0x18(%rsp),%rdi
 141:	48 03 7c 24 20       	add    0x20(%rsp),%rdi
 146:	48 03 7c 24 28       	add    0x28(%rsp),%rdi
 14b:	48 89 7c 24 48       	mov    %rdi,0x48(%rsp)
 150:	48 8b 44 24 30       	mov    0x30(%rsp),%rax
 155:	48 8b 54 24 38       	mov    0x38(%rsp),%rdx
 15a:	48 8b 4c 24 40       	mov    0x40(%rsp),%rcx
 15f:	4c 8b 44 24 48       	mov    0x48(%rsp),%r8
 164:	48 81 c4 58 00 00 00 	add    $0x58,%rsp
 16b:	c3                   	ret
 16c:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 171:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 175:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 17c:	89 46 14             	mov    %eax,0x14(%rsi)
 17f:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 185:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 189:	c3                   	ret
