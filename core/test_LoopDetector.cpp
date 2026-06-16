#include "LoopDetector.hpp"
#include <cassert>
#include <iostream>

void run_tests() {
    LoopDetector detector;

    std::cout << "Running LoopDetector tests..." << std::endl;

    // Test 1: No loop with different calls
    assert(detector.checkLoop("get_weather", "{\"location\":\"Paris\"}") == false);
    assert(detector.checkLoop("get_weather", "{\"location\":\"London\"}") == false);
    assert(detector.checkLoop("get_weather", "{\"location\":\"Berlin\"}") == false);
    
    detector.reset();

    // Test 2: Trigger loop at exactly 3 consecutive identical calls
    assert(detector.checkLoop("get_time", "{\"timezone\":\"UTC\"}") == false); // Call 1
    assert(detector.checkLoop("get_time", "{\"timezone\":\"UTC\"}") == false); // Call 2
    assert(detector.checkLoop("get_time", "{\"timezone\":\"UTC\"}") == true);  // Call 3 (Loop!)
    assert(detector.checkLoop("get_time", "{\"timezone\":\"UTC\"}") == true);  // Call 4 (Still loop)
    
    detector.reset();

    // Test 3: Interrupted sequence shouldn't trigger loop
    assert(detector.checkLoop("calculate", "{\"expr\":\"2+2\"}") == false);
    assert(detector.checkLoop("calculate", "{\"expr\":\"2+2\"}") == false);
    assert(detector.checkLoop("calculate", "{\"expr\":\"3+3\"}") == false); // Interruption
    assert(detector.checkLoop("calculate", "{\"expr\":\"2+2\"}") == false); // Starts over

    std::cout << "All LoopDetector tests passed successfully!" << std::endl;
}

int main() {
    run_tests();
    return 0;
}
