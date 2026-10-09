
/home/jtenner/Projects/wago-sum-unroll/experiments/specialized-sum-unroll/results/mitigation/native-corpus-baseline/memory.bin:     file format binary


Disassembly of section .data:

0000000000000000 <.data>:
   0:	48 89 f3             	mov    %rsi,%rbx
   3:	4c 8b bb e0 fe ff ff 	mov    -0x120(%rbx),%r15
   a:	51                   	push   %rcx
   b:	48 8b 07             	mov    (%rdi),%rax
   e:	e8 02 00 00 00       	call   0x15
  13:	59                   	pop    %rcx
  14:	c3                   	ret
  15:	48 81 ec 00 00 00 00 	sub    $0x0,%rsp
  1c:	41 89 c4             	mov    %eax,%r12d
  1f:	31 c0                	xor    %eax,%eax
  21:	45 31 ed             	xor    %r13d,%r13d
  24:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  2b:	00 00 
  2d:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  34:	00 00 
  36:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  3d:	00 00 
  3f:	90                   	nop
  40:	45 85 e4             	test   %r12d,%r12d
  43:	0f 84 2a 00 00 00    	je     0x73
  49:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  4e:	44 89 e7             	mov    %r12d,%edi
  51:	45 89 ed             	mov    %r13d,%r13d
  54:	49 8d 75 08          	lea    0x8(%r13),%rsi
  58:	4c 39 fe             	cmp    %r15,%rsi
  5b:	0f 87 1a 00 00 00    	ja     0x7b
  61:	4a 89 3c 2b          	mov    %rdi,(%rbx,%r13,1)
  65:	41 83 c5 08          	add    $0x8,%r13d
  69:	41 83 ec 01          	sub    $0x1,%r12d
  6d:	0f 85 db ff ff ff    	jne    0x4e
  73:	48 81 c4 00 00 00 00 	add    $0x0,%rsp
  7a:	c3                   	ret
  7b:	b8 11 00 00 00       	mov    $0x11,%eax
  80:	e9 00 00 00 00       	jmp    0x85
  85:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
  89:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
  90:	89 46 14             	mov    %eax,0x14(%rsi)
  93:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
  99:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
  9d:	c3                   	ret
  9e:	00 00                	add    %al,(%rax)
  a0:	48 89 f3             	mov    %rsi,%rbx
  a3:	4c 8b bb e0 fe ff ff 	mov    -0x120(%rbx),%r15
  aa:	51                   	push   %rcx
  ab:	48 8b 07             	mov    (%rdi),%rax
  ae:	e8 05 00 00 00       	call   0xb8
  b3:	59                   	pop    %rcx
  b4:	48 89 01             	mov    %rax,(%rcx)
  b7:	c3                   	ret
  b8:	48 81 ec 28 00 00 00 	sub    $0x28,%rsp
  bf:	41 89 c4             	mov    %eax,%r12d
  c2:	31 c0                	xor    %eax,%eax
  c4:	45 31 ed             	xor    %r13d,%r13d
  c7:	41 be 00 00 00 00    	mov    $0x0,%r14d
  cd:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  d4:	00 00 
  d6:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  dd:	00 00 
  df:	90                   	nop
  e0:	45 85 e4             	test   %r12d,%r12d
  e3:	0f 84 bf 00 00 00    	je     0x1a8
  e9:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  ee:	44 89 e7             	mov    %r12d,%edi
  f1:	48 c1 e7 03          	shl    $0x3,%rdi
  f5:	45 89 ed             	mov    %r13d,%r13d
  f8:	4c 01 ef             	add    %r13,%rdi
  fb:	4c 39 ff             	cmp    %r15,%rdi
  fe:	0f 86 20 00 00 00    	jbe    0x124
 104:	48 bf 00 00 00 00 01 	movabs $0x100000000,%rdi
 10b:	00 00 00 
 10e:	4c 39 ff             	cmp    %r15,%rdi
 111:	0f 85 a3 00 00 00    	jne    0x1ba
 117:	41 f7 c5 07 00 00 00 	test   $0x7,%r13d
 11e:	0f 85 96 00 00 00    	jne    0x1ba
 124:	4e 03 34 2b          	add    (%rbx,%r13,1),%r14
 128:	41 83 c5 08          	add    $0x8,%r13d
 12c:	31 ff                	xor    %edi,%edi
 12e:	31 f6                	xor    %esi,%esi
 130:	31 ed                	xor    %ebp,%ebp
 132:	41 83 ec 01          	sub    $0x1,%r12d
 136:	0f 84 63 00 00 00    	je     0x19f
 13c:	41 83 fc 04          	cmp    $0x4,%r12d
 140:	0f 82 3e 00 00 00    	jb     0x184
 146:	44 89 e7             	mov    %r12d,%edi
 149:	48 c1 e7 03          	shl    $0x3,%rdi
 14d:	4c 01 ef             	add    %r13,%rdi
 150:	48 c1 ef 20          	shr    $0x20,%rdi
 154:	bf 00 00 00 00       	mov    $0x0,%edi
 159:	0f 85 25 00 00 00    	jne    0x184
 15f:	4e 03 34 2b          	add    (%rbx,%r13,1),%r14
 163:	4a 03 7c 2b 08       	add    0x8(%rbx,%r13,1),%rdi
 168:	4a 03 74 2b 10       	add    0x10(%rbx,%r13,1),%rsi
 16d:	4a 03 6c 2b 18       	add    0x18(%rbx,%r13,1),%rbp
 172:	41 83 c5 20          	add    $0x20,%r13d
 176:	41 83 ec 04          	sub    $0x4,%r12d
 17a:	41 83 fc 04          	cmp    $0x4,%r12d
 17e:	0f 83 db ff ff ff    	jae    0x15f
 184:	45 85 e4             	test   %r12d,%r12d
 187:	0f 84 12 00 00 00    	je     0x19f
 18d:	4e 03 34 2b          	add    (%rbx,%r13,1),%r14
 191:	41 83 c5 08          	add    $0x8,%r13d
 195:	41 83 ec 01          	sub    $0x1,%r12d
 199:	0f 85 ee ff ff ff    	jne    0x18d
 19f:	49 01 fe             	add    %rdi,%r14
 1a2:	49 01 f6             	add    %rsi,%r14
 1a5:	49 01 ee             	add    %rbp,%r14
 1a8:	4c 89 74 24 10       	mov    %r14,0x10(%rsp)
 1ad:	48 8b 44 24 10       	mov    0x10(%rsp),%rax
 1b2:	48 81 c4 28 00 00 00 	add    $0x28,%rsp
 1b9:	c3                   	ret
 1ba:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 1bf:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 1c3:	c7 46 10 02 00 00 00 	movl   $0x2,0x10(%rsi)
 1ca:	89 46 14             	mov    %eax,0x14(%rsi)
 1cd:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 1d3:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 1d7:	c3                   	ret
