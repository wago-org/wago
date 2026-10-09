
experiments/loop-unroll-vectorization/results/native-modern/map-i32-count4.bin:     file format binary


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
  1e:	e8 1d 00 00 00       	call   0x40
  23:	5f                   	pop    %rdi
  24:	48 89 07             	mov    %rax,(%rdi)
  27:	48 89 57 08          	mov    %rdx,0x8(%rdi)
  2b:	48 89 4f 10          	mov    %rcx,0x10(%rdi)
  2f:	4c 89 47 18          	mov    %r8,0x18(%rdi)
  33:	c3                   	ret
  34:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  3b:	00 00 
  3d:	0f 1f 00             	nopl   (%rax)
  40:	48 81 ec 48 00 00 00 	sub    $0x48,%rsp
  47:	41 89 c4             	mov    %eax,%r12d
  4a:	41 89 cd             	mov    %ecx,%r13d
  4d:	41 89 d6             	mov    %edx,%r14d
  50:	45 89 c3             	mov    %r8d,%r11d
  53:	44 89 cd             	mov    %r9d,%ebp
  56:	31 c0                	xor    %eax,%eax
  58:	45 31 d2             	xor    %r10d,%r10d
  5b:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  62:	00 00 
  64:	0f 1f 40 00          	nopl   0x0(%rax)
  68:	41 83 fe 04          	cmp    $0x4,%r14d
  6c:	0f 82 e8 00 00 00    	jb     0x15a
  72:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  77:	45 89 ed             	mov    %r13d,%r13d
  7a:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  7e:	4c 39 ff             	cmp    %r15,%rdi
  81:	0f 87 5b 01 00 00    	ja     0x1e2
  87:	46 8b 14 2b          	mov    (%rbx,%r13,1),%r10d
  8b:	45 0f af d3          	imul   %r11d,%r10d
  8f:	41 01 ea             	add    %ebp,%r10d
  92:	45 89 e4             	mov    %r12d,%r12d
  95:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  9a:	4c 39 ff             	cmp    %r15,%rdi
  9d:	0f 87 3f 01 00 00    	ja     0x1e2
  a3:	46 89 14 23          	mov    %r10d,(%rbx,%r12,1)
  a7:	41 83 c4 04          	add    $0x4,%r12d
  ab:	41 83 c5 04          	add    $0x4,%r13d
  af:	41 83 ee 01          	sub    $0x1,%r14d
  b3:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  b7:	4c 39 ff             	cmp    %r15,%rdi
  ba:	0f 87 22 01 00 00    	ja     0x1e2
  c0:	46 8b 14 2b          	mov    (%rbx,%r13,1),%r10d
  c4:	45 0f af d3          	imul   %r11d,%r10d
  c8:	41 01 ea             	add    %ebp,%r10d
  cb:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  d0:	4c 39 ff             	cmp    %r15,%rdi
  d3:	0f 87 09 01 00 00    	ja     0x1e2
  d9:	46 89 14 23          	mov    %r10d,(%rbx,%r12,1)
  dd:	41 83 c4 04          	add    $0x4,%r12d
  e1:	41 83 c5 04          	add    $0x4,%r13d
  e5:	41 83 ee 01          	sub    $0x1,%r14d
  e9:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  ed:	4c 39 ff             	cmp    %r15,%rdi
  f0:	0f 87 ec 00 00 00    	ja     0x1e2
  f6:	46 8b 14 2b          	mov    (%rbx,%r13,1),%r10d
  fa:	45 0f af d3          	imul   %r11d,%r10d
  fe:	41 01 ea             	add    %ebp,%r10d
 101:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
 106:	4c 39 ff             	cmp    %r15,%rdi
 109:	0f 87 d3 00 00 00    	ja     0x1e2
 10f:	46 89 14 23          	mov    %r10d,(%rbx,%r12,1)
 113:	41 83 c4 04          	add    $0x4,%r12d
 117:	41 83 c5 04          	add    $0x4,%r13d
 11b:	41 83 ee 01          	sub    $0x1,%r14d
 11f:	49 8d 7d 04          	lea    0x4(%r13),%rdi
 123:	4c 39 ff             	cmp    %r15,%rdi
 126:	0f 87 b6 00 00 00    	ja     0x1e2
 12c:	46 8b 14 2b          	mov    (%rbx,%r13,1),%r10d
 130:	45 0f af d3          	imul   %r11d,%r10d
 134:	41 01 ea             	add    %ebp,%r10d
 137:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
 13c:	4c 39 ff             	cmp    %r15,%rdi
 13f:	0f 87 9d 00 00 00    	ja     0x1e2
 145:	46 89 14 23          	mov    %r10d,(%rbx,%r12,1)
 149:	41 83 c4 04          	add    $0x4,%r12d
 14d:	41 83 c5 04          	add    $0x4,%r13d
 151:	41 83 ee 01          	sub    $0x1,%r14d
 155:	e9 0e ff ff ff       	jmp    0x68
 15a:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
 161:	00 00 
 163:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
 168:	45 85 f6             	test   %r14d,%r14d
 16b:	0f 84 41 00 00 00    	je     0x1b2
 171:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
 176:	49 8d 7d 04          	lea    0x4(%r13),%rdi
 17a:	4c 39 ff             	cmp    %r15,%rdi
 17d:	0f 87 5f 00 00 00    	ja     0x1e2
 183:	46 8b 14 2b          	mov    (%rbx,%r13,1),%r10d
 187:	45 0f af d3          	imul   %r11d,%r10d
 18b:	41 01 ea             	add    %ebp,%r10d
 18e:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
 193:	4c 39 ff             	cmp    %r15,%rdi
 196:	0f 87 46 00 00 00    	ja     0x1e2
 19c:	46 89 14 23          	mov    %r10d,(%rbx,%r12,1)
 1a0:	41 83 c4 04          	add    $0x4,%r12d
 1a4:	41 83 c5 04          	add    $0x4,%r13d
 1a8:	41 83 ee 01          	sub    $0x1,%r14d
 1ac:	0f 85 c4 ff ff ff    	jne    0x176
 1b2:	4c 89 64 24 18       	mov    %r12,0x18(%rsp)
 1b7:	4c 89 6c 24 20       	mov    %r13,0x20(%rsp)
 1bc:	4c 89 74 24 28       	mov    %r14,0x28(%rsp)
 1c1:	4c 89 54 24 30       	mov    %r10,0x30(%rsp)
 1c6:	48 8b 44 24 18       	mov    0x18(%rsp),%rax
 1cb:	48 8b 54 24 20       	mov    0x20(%rsp),%rdx
 1d0:	48 8b 4c 24 28       	mov    0x28(%rsp),%rcx
 1d5:	4c 8b 44 24 30       	mov    0x30(%rsp),%r8
 1da:	48 81 c4 48 00 00 00 	add    $0x48,%rsp
 1e1:	c3                   	ret
 1e2:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 1e7:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 1eb:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 1f2:	89 46 14             	mov    %eax,0x14(%rsi)
 1f5:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 1fb:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 1ff:	c3                   	ret
