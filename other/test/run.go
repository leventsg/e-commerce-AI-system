package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, " ")
		nums := make([]int, 0, len(parts))
		for _, part := range parts {
			num, _ := strconv.Atoi(part)
			nums = append(nums, num)
		}
		quickSort(nums, 0, len(nums)-1)
		fmt.Println(nums)
	}
}

func quickSort(nums []int, l, r int) {
	if l >= r {
		return
	}
	x := nums[l]
	i := l
	for j := l; j <= r; j++ {
		if nums[j] <= x {
			nums[i], nums[j] = nums[j], nums[i]
			i++
		}
	}

	nums[i-1], nums[l] = nums[l], nums[i-1]

	quickSort(nums, l, i-2)
	quickSort(nums, i, r)
}
